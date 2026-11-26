package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Store 基于 SQLite 的分队历史存储。
type Store struct {
	db *sql.DB
}

// HistorySummary 是历史列表行，只含汇总字段，避免一次拉出全部大 JSON。
type HistorySummary struct {
	ID                int64     `json:"id"`
	CreatedAt         time.Time `json:"createdAt"`
	Seed              int64     `json:"seed"`
	RosterCount       int       `json:"rosterCount"`
	StarterCount      int       `json:"starterCount"`
	BenchCount        int       `json:"benchCount"`
	ScoreA            int       `json:"scoreA"`
	ScoreB            int       `json:"scoreB"`
	PositionConflicts int       `json:"positionConflicts"`
	// Outcome 为战后录入的战果："" 未录入、"win" 天辉胜、"lose" 天辉负（夜魇胜）。
	Outcome string   `json:"outcome"`
	BestOf  int      `json:"bestOf"`
	Rounds  []string `json:"rounds,omitempty"`
}

// HistoryDetail 是单条历史详情，附带当时的请求阵容与完整分队结果。
type HistoryDetail struct {
	HistorySummary
	Request    balanceRequest `json:"request"`
	Result     Result         `json:"result"`
	resultJSON string
}

func userDBPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "dota-shuffle")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "dota-shuffle.db"), nil
}

// DefaultDBPath 决定数据库文件位置：优先 exe 同目录（绿色版便携），
// 该目录不可写时回退到当前用户的配置目录。
func DefaultDBPath() string {
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		if dirWritable(dir) {
			return filepath.Join(dir, "dota-shuffle.db")
		}
	}
	if path, err := userDBPath(); err == nil {
		return path
	}
	return "dota-shuffle.db"
}

// dirWritable 用临时文件探测目录是否可写，失败常见于 Program Files 等受保护位置。
func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".dota-write-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

// OpenStore 打开（必要时创建）数据库并执行建表迁移。
func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	// 单文件桌面工具，串行访问即可，避免 WAL 下多连接锁竞争。
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close 释放数据库连接。
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	const ddl = `
CREATE TABLE IF NOT EXISTS shuffle_records (
	id                  INTEGER PRIMARY KEY AUTOINCREMENT,
	created_at          TEXT    NOT NULL,
	seed                INTEGER NOT NULL,
	roster_count        INTEGER NOT NULL,
	starter_count       INTEGER NOT NULL,
	bench_count         INTEGER NOT NULL,
	score_a             INTEGER NOT NULL,
	score_b             INTEGER NOT NULL,
	position_conflicts  INTEGER NOT NULL,
	outcome             TEXT    NOT NULL DEFAULT '',
	best_of             INTEGER NOT NULL DEFAULT 1,
	rounds              TEXT    NOT NULL DEFAULT '',
	request_json        TEXT    NOT NULL,
	result_json         TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_shuffle_records_created_at ON shuffle_records(created_at DESC);
`
	if _, err := s.db.Exec(ddl); err != nil {
		return err
	}
	// SQLite 不支持 ADD COLUMN IF NOT EXISTS，兼容已有的单局历史。
	for _, column := range []string{
		"outcome TEXT NOT NULL DEFAULT ''",
		"best_of INTEGER NOT NULL DEFAULT 1",
		"rounds TEXT NOT NULL DEFAULT ''",
	} {
		if _, err := s.db.Exec("ALTER TABLE shuffle_records ADD COLUMN " + column); err != nil &&
			!strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	return nil
}

// SaveRecord 持久化一次分队：请求阵容与完整结果都原样存 JSON，汇总字段冗余成列便于列表查询。
// 返回新记录的自增 ID。
func (s *Store) SaveRecord(req *balanceRequest, res *Result) (int64, error) {
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return 0, err
	}
	resJSON, err := json.Marshal(res)
	if err != nil {
		return 0, err
	}
	starters := len(res.A.Players) + len(res.B.Players)
	summary := HistorySummary{
		CreatedAt:         time.Now(),
		Seed:              req.Seed,
		RosterCount:       len(req.Players),
		StarterCount:      starters,
		BenchCount:        len(res.Bench),
		ScoreA:            res.ScoreA,
		ScoreB:            res.ScoreB,
		PositionConflicts: res.ConflictA + res.ConflictB,
	}
	bestOf := 1
	if req.BestOf == 3 {
		bestOf = 3
	}
	ret, err := s.db.Exec(`
INSERT INTO shuffle_records
	(created_at, seed, roster_count, starter_count, bench_count, score_a, score_b, position_conflicts, outcome, best_of, request_json, result_json)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		summary.CreatedAt.Format(time.RFC3339), summary.Seed, summary.RosterCount,
		summary.StarterCount, summary.BenchCount, summary.ScoreA, summary.ScoreB,
		summary.PositionConflicts, "", bestOf, string(reqJSON), string(resJSON))
	if err != nil {
		return 0, err
	}
	return ret.LastInsertId()
}

// ListRecords 按时间倒序返回最近 limit 条汇总记录。
func (s *Store) ListRecords(limit int) ([]HistorySummary, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`
SELECT id, created_at, seed, roster_count, starter_count, bench_count, score_a, score_b, position_conflicts, outcome, best_of, rounds
FROM shuffle_records
ORDER BY id DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]HistorySummary, 0, limit)
	for rows.Next() {
		var (
			item      HistorySummary
			createdAt string
			rounds    string
		)
		if err := rows.Scan(&item.ID, &createdAt, &item.Seed, &item.RosterCount,
			&item.StarterCount, &item.BenchCount, &item.ScoreA, &item.ScoreB,
			&item.PositionConflicts, &item.Outcome, &item.BestOf, &rounds); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			item.CreatedAt = t
		}
		item.Rounds = parseRounds(rounds)
		out = append(out, item)
	}
	return out, rows.Err()
}

// GetRecord 按 ID 取单条详情，并把存储的 JSON 反序列化回请求与结果结构。
func (s *Store) GetRecord(id int64) (*HistoryDetail, error) {
	var (
		d                                   HistoryDetail
		createdAt, rounds, reqJSON, resJSON string
	)
	err := s.db.QueryRow(`
SELECT id, created_at, seed, roster_count, starter_count, bench_count, score_a, score_b, position_conflicts, outcome, best_of, rounds, request_json, result_json
FROM shuffle_records WHERE id = ?`, id).Scan(
		&d.ID, &createdAt, &d.Seed, &d.RosterCount, &d.StarterCount, &d.BenchCount,
		&d.ScoreA, &d.ScoreB, &d.PositionConflicts, &d.Outcome, &d.BestOf, &rounds, &reqJSON, &resJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		d.CreatedAt = t
	}
	if err := json.Unmarshal([]byte(reqJSON), &d.Request); err != nil {
		return nil, fmt.Errorf("解析历史请求失败: %w", err)
	}
	if err := json.Unmarshal([]byte(resJSON), &d.Result); err != nil {
		return nil, fmt.Errorf("解析历史结果失败: %w", err)
	}
	d.Rounds = parseRounds(rounds)
	d.resultJSON = resJSON
	for _, team := range []*Team{&d.Result.A, &d.Result.B} {
		if team.Assignments == nil {
			assignTeamPositions(team)
		}
	}
	d.Result.RecordID = d.ID
	return &d, nil
}

func (s *Store) saveSubstitution(detail *HistoryDetail) (bool, error) {
	request, err := json.Marshal(detail.Request)
	if err != nil {
		return false, err
	}
	result, err := json.Marshal(detail.Result)
	if err != nil {
		return false, err
	}
	update, err := s.db.Exec(`UPDATE shuffle_records
SET request_json = ?, result_json = ?, score_a = ?, score_b = ?, position_conflicts = ?
WHERE id = ? AND best_of = 3 AND outcome = '' AND result_json = ? AND rounds = ?`,
		string(request), string(result), detail.Result.ScoreA, detail.Result.ScoreB,
		detail.Result.ConflictA+detail.Result.ConflictB, detail.ID, detail.resultJSON, strings.Join(detail.Rounds, ","))
	if err != nil {
		return false, err
	}
	count, err := update.RowsAffected()
	return count == 1, err
}

// DeleteRecord 按 ID 删除一条历史，返回是否实际删到了行。
func (s *Store) DeleteRecord(id int64) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM shuffle_records WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected > 0, err
}

// SetOutcome 录入或更新一条历史的战果：win 表示天辉胜，lose 表示天辉负（夜魇胜），
// 空串表示清除战果。返回是否实际更新到行。
func (s *Store) SetOutcome(id int64, outcome string) (bool, error) {
	res, err := s.db.Exec(`UPDATE shuffle_records SET outcome = ? WHERE id = ? AND best_of = 1`, outcome, id)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected > 0, err
}

func parseRounds(stored string) []string {
	if stored == "" {
		return nil
	}
	return strings.Split(stored, ",")
}

func seriesWinner(rounds []string) string {
	var radiant, dire int
	for _, outcome := range rounds {
		if outcome == "win" {
			radiant++
		} else {
			dire++
		}
	}
	if radiant == 2 {
		return "win"
	}
	if dire == 2 {
		return "lose"
	}
	return ""
}

// updateSeriesRounds 用前一版本做条件更新，避免两个窗口重复录入同一局。
func (s *Store) updateSeriesRounds(detail *HistoryDetail, rounds []string) (bool, error) {
	result, err := s.db.Exec(`UPDATE shuffle_records SET rounds = ?, outcome = ?
WHERE id = ? AND best_of = 3 AND rounds = ? AND result_json = ?`,
		strings.Join(rounds, ","), seriesWinner(rounds), detail.ID, strings.Join(detail.Rounds, ","), detail.resultJSON)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}
