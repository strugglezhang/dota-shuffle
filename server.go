package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"slices"
	"strconv"
)

// webFS 把前端页面与 Vue 运行时打进二进制，交付时只需分发一个可执行文件。
//
//go:embed web
var webFS embed.FS

// balanceRequest 是 /api/balance 的请求体。
type balanceRequest struct {
	Players []Player `json:"players"`
	Seed    int64    `json:"seed"`
	BestOf  int      `json:"bestOf,omitempty"`
}

// apiServer 聚合 HTTP 处理器与其依赖的历史存储；store 为 nil 时接口不持久化。
type apiServer struct {
	store *Store
}

// newMux 注册全部路由，桌面窗口与浏览器模式共用同一套 Handler。
func newMux(s *apiServer) (http.Handler, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/balance", s.handleBalance)
	mux.HandleFunc("GET /api/history", s.handleHistory)
	mux.HandleFunc("GET /api/history/{id}", s.handleHistoryItem)
	mux.HandleFunc("DELETE /api/history/{id}", s.handleHistoryItem)
	mux.HandleFunc("PUT /api/history/{id}/outcome", s.handleHistoryOutcome)
	mux.HandleFunc("PUT /api/history/{id}/rounds", s.handleSeriesRounds)
	mux.HandleFunc("PUT /api/history/{id}/substitution", s.handleSubstitution)
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	static, err := fs.Sub(webFS, "web")
	if err != nil {
		return nil, err
	}
	mux.Handle("GET /", http.FileServer(http.FS(static)))
	return mux, nil
}

// serve 启动静态页面与分队 API 的 HTTP 服务。
func serve(addr string, store *Store) error {
	mux, err := newMux(&apiServer{store: store})
	if err != nil {
		return err
	}
	return http.ListenAndServe(addr, mux)
}

// handleBalance 复用与 CLI 完全相同的归一化与枚举算法，保证两条入口结果一致；
// 成功后在配置了存储时把本次阵容与结果写入 SQLite。
func (s *apiServer) handleBalance(w http.ResponseWriter, r *http.Request) {
	var req balanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	if req.BestOf != 0 && req.BestOf != 1 && req.BestOf != 3 {
		writeAPIError(w, http.StatusBadRequest, "仅支持单局或 BO3")
		return
	}
	roster := &Roster{Players: req.Players}
	if err := roster.normalize(); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := Balance(roster.Players, req.Seed)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.store != nil {
		id, err := s.store.SaveRecord(&req, result)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "保存分队记录失败: "+err.Error())
			return
		}
		result.RecordID = id
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(result)
}

// handleHistory 返回最近的分队历史汇总列表。
func (s *apiServer) handleHistory(w http.ResponseWriter, _ *http.Request) {
	if s.store == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "当前模式未启用历史存储")
		return
	}
	items, err := s.store.ListRecords(50)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "读取历史记录失败: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"records": items})
}

// handleHistoryItem 处理单条历史的查看（GET）与删除（DELETE）。
func (s *apiServer) handleHistoryItem(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "当前模式未启用历史存储")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "历史记录 ID 非法")
		return
	}

	switch r.Method {
	case http.MethodDelete:
		ok, err := s.store.DeleteRecord(id)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "删除历史记录失败: "+err.Error())
			return
		}
		if !ok {
			writeAPIError(w, http.StatusNotFound, "历史记录不存在或已删除")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		detail, err := s.store.GetRecord(id)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "读取历史详情失败: "+err.Error())
			return
		}
		if detail == nil {
			writeAPIError(w, http.StatusNotFound, "历史记录不存在")
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(detail)
	}
}

// handleHistoryOutcome 录入/修改一场对局的战果：win=天辉胜，lose=天辉负（夜魇胜），空串=清除。
func (s *apiServer) handleHistoryOutcome(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "当前模式未启用历史存储")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "历史记录 ID 非法")
		return
	}
	var body struct {
		Outcome string `json:"outcome"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	switch body.Outcome {
	case "", "win", "lose":
	default:
		writeAPIError(w, http.StatusBadRequest, "outcome 只能是 win、lose 或空串")
		return
	}
	updated, err := s.store.SetOutcome(id, body.Outcome)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "更新战果失败: "+err.Error())
		return
	}
	if !updated {
		writeAPIError(w, http.StatusNotFound, "历史记录不存在或已删除")
		return
	}
	detail, err := s.store.GetRecord(id)
	if err != nil || detail == nil {
		writeAPIError(w, http.StatusInternalServerError, "读取更新后的历史记录失败")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(detail.HistorySummary)
}

// handleSeriesRounds 录入下一局或撤销最后一局。两胜后结束系列赛。
func (s *apiServer) handleSeriesRounds(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "当前模式未启用历史存储")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "历史记录 ID 非法")
		return
	}
	var req struct {
		Previous []string `json:"previous"`
		Outcome  string   `json:"outcome"` // win/lose 追加一局，undo 撤销最后一局
		Revision *int     `json:"revision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	detail, err := s.store.GetRecord(id)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "读取 BO3 失败")
		return
	}
	if detail == nil || detail.BestOf != 3 {
		writeAPIError(w, http.StatusNotFound, "BO3 记录不存在")
		return
	}
	if !slices.Equal(req.Previous, detail.Rounds) ||
		(req.Revision != nil && *req.Revision != len(detail.Result.Substitutions)) {
		writeAPIError(w, http.StatusConflict, "BO3 已更新，请重新打开该系列赛")
		return
	}
	rounds := detail.Rounds
	switch req.Outcome {
	case "win", "lose":
		if len(rounds) >= 3 || seriesWinner(rounds) != "" {
			writeAPIError(w, http.StatusBadRequest, "本场 BO3 已结束")
			return
		}
		rounds = append(slices.Clone(rounds), req.Outcome)
	case "undo":
		if len(rounds) == 0 {
			writeAPIError(w, http.StatusBadRequest, "还没有战果可以撤销")
			return
		}
		rounds = rounds[:len(rounds)-1]
	default:
		writeAPIError(w, http.StatusBadRequest, "无效的战果")
		return
	}

	updated, err := s.store.updateSeriesRounds(detail, rounds)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "保存 BO3 战果失败")
		return
	}
	if !updated {
		writeAPIError(w, http.StatusConflict, "战果已更新，请重新打开该系列赛")
		return
	}
	detail.Rounds = rounds
	detail.Outcome = seriesWinner(rounds)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(detail.HistorySummary)
}

// apiError 是统一的错误响应结构。
type apiError struct {
	Error string `json:"error"`
}

func writeAPIError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiError{Error: msg})
}
