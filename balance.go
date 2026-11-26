package main

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
)

// Team 是一支分好的队伍。
type Team struct {
	Name        string         `json:"name"`
	Players     []Player       `json:"players"`
	Assignments map[string]int `json:"assignments,omitempty"` // 昵称对应本局位置，0 表示仍需补位
}

// Result 是一次分队的完整结果与均衡指标。
type Result struct {
	A Team `json:"teamA"`
	B Team `json:"teamB"`
	// Bench 为替补席：报名超过 10 人时，按录入顺序从第 11 人起进入替补，不参与首发分队。
	Bench []Player `json:"bench,omitempty"`
	// 汇总指标
	ScoreA int `json:"scoreA"` // 天辉有效总分
	ScoreB int `json:"scoreB"` // 夜魇有效总分
	TierA  int `json:"tierA"`  // 天辉段位档位之和
	TierB  int `json:"tierB"`  // 夜魇段位档位之和
	// 位置冲突：同一队中同一位置出现多个人时，多出来的人次。
	ConflictA int `json:"conflictA"`
	ConflictB int `json:"conflictB"`
	ScoreDiff int `json:"scoreDiff"` // 两队有效总分之差的绝对值
	Cost      int `json:"cost"`      // 历史兼容字段，分队按冲突数和分差分别比较
	// RecordID 是持久化后的历史记录 ID，仅 HTTP 接口写入 SQLite 后回填；CLI 等无库场景为零值并省略。
	RecordID      int64          `json:"recordID,omitempty"`
	Substitutions []Substitution `json:"substitutions,omitempty"`
}

type Substitution struct {
	Incoming  string `json:"incoming"`
	Outgoing  string `json:"outgoing"`
	Team      string `json:"team"`
	FromRound int    `json:"fromRound"`
}

// 保留旧版 JSON 的成本字段，实际排序独立比较冲突数与分差。
const weightPositionConflict = 100000

// maxRosterSize 是报名总人数上限：10 人首发之外，最多再收 10 人替补。
const maxRosterSize = 20

// starterLimit 是一局对战的首发总人数：Dota2 两队各 5 人，共 10 人。
// 报名超过该人数时，按录入顺序取前 10 人首发，其余进入替补席。
const starterLimit = 10
const maxStarterMMR = math.MaxInt / starterLimit

// starterCount 返回报名 n 人中进入首发的人数，上限 10；替补数为 n - starterCount(n)。
func starterCount(n int) int {
	if n > starterLimit {
		return starterLimit
	}
	return n
}

// Balance 枚举等分方案，先最小化位置冲突，再最小化有效分差。
//
// 枚举思路：首发 N 名玩家取 N/2 人组成天辉，其余为夜魇。用位掩码扫描所有
// 组合（10 人时至多 C(10,5)=252 种），逐个比较位置冲突和分差，
// 因此对开黑规模总能给出全局最优，而不是贪心近似。
//
// 当报名人数超过 10 人时，严格按录入顺序取前 10 人作为首发参与枚举，
// 第 11 人及之后原样进入 Result.Bench 替补席，不参与均衡计算。
//
// seed <= 0 时结果完全确定（多个等价最优解取掩码最小者）；
// seed > 0 时在全部等价最优解中按种子随机挑一个，增加“摇人”的随机性。
func Balance(players []Player, seed int64) (*Result, error) {
	n := len(players)
	if n < 2 || n > maxRosterSize || starterCount(n)%2 != 0 {
		return nil, errBadRosterSize(n)
	}

	// 首发/替补切分：仅首发玩家进入枚举，替补保持录入顺序原样返回。
	starters := players
	var bench []Player
	if n > starterLimit {
		starters = players[:starterLimit]
		bench = players[starterLimit:]
	}
	m := len(starters)
	half := m / 2

	for _, p := range starters {
		if p.EffMMR < 0 || p.EffMMR > maxStarterMMR {
			return nil, fmt.Errorf("玩家 %s 的有效分数超出可计算范围", p.Name)
		}
	}

	var bestMasks []int
	bestConflict, bestDiff := math.MaxInt, math.MaxInt

	// 强制 0 号玩家进天辉，消除“A/B 互换”的对称重复，枚举量减半且不丢方案。
	for mask := 1; mask < (1 << m); mask += 2 {
		if popCount(mask) != half {
			continue
		}
		conflict, diff := teamMetrics(starters, mask)
		if conflict < bestConflict || (conflict == bestConflict && diff < bestDiff) {
			bestConflict, bestDiff = conflict, diff
			bestMasks = []int{mask}
		} else if conflict == bestConflict && diff == bestDiff {
			bestMasks = append(bestMasks, mask)
		}
	}

	chosen := bestMasks[0]
	if seed > 0 {
		random := rand.New(rand.NewSource(seed))
		chosen = bestMasks[random.Intn(len(bestMasks))]
		if random.Intn(2) == 1 {
			chosen ^= (1 << m) - 1
		}
	}
	res := buildResult(starters, chosen)
	res.Bench = bench
	return res, nil
}

// errBadRosterSize 把人数校验错误集中表达，避免算法层重复拼字符串。
func errBadRosterSize(n int) error {
	if n < 2 {
		return &BalanceError{Msg: "人数不足，至少需要 2 人"}
	}
	if n > maxRosterSize {
		return &BalanceError{Msg: fmt.Sprintf("报名人数 %d 超过上限 %d（10 首发 + %d 替补）", n, maxRosterSize, maxRosterSize-starterLimit)}
	}
	return &BalanceError{Msg: fmt.Sprintf("当前 %d 人，首发人数必须为偶数才能平分两队（10 人以内请凑成偶数）", n)}
}

// BalanceError 是分队阶段的业务错误。
type BalanceError struct{ Msg string }

func (e *BalanceError) Error() string { return e.Msg }

// popCount 统计整数中置位的比特数。
func popCount(x int) int {
	c := 0
	for x != 0 {
		x &= x - 1
		c++
	}
	return c
}

func teamMetrics(players []Player, maskA int) (conflicts, diff int) {
	var scoreA, scoreB int
	var teamA, teamB []Player

	for i, p := range players {
		if maskA&(1<<i) != 0 {
			scoreA += p.EffMMR
			teamA = append(teamA, p)
		} else {
			scoreB += p.EffMMR
			teamB = append(teamB, p)
		}
	}

	return teamPositionConflict(teamA) + teamPositionConflict(teamB), absInt(scoreA - scoreB)
}

// matchPositions 用增广路求队内玩家到 1-5 号位置的最大匹配：
// 每名玩家只能落到自己可打位置中的一个，每个位置最多容纳一人。
// 队伍规模恒 ≤5，直接 DFS 即可，无需更重的算法库。
func matchPositions(team []Player) (owners [6]int, count int) {
	// 0 表示空位，其他值为玩家下标加一。
	var assign func(int, *[6]bool) bool
	assign = func(player int, seen *[6]bool) bool {
		for _, pos := range playerPositions(team[player-1]) {
			if pos < 1 || pos > 5 || seen[pos] {
				continue
			}
			seen[pos] = true
			if owners[pos] == 0 || assign(owners[pos], seen) {
				owners[pos] = player
				return true
			}
		}
		return false
	}
	for i := range team {
		if assign(i+1, &[6]bool{}) {
			count++
		}
	}
	return owners, count
}

// absInt 返回整数绝对值。
func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// buildResult 依据天辉掩码组装对外结果，并按位置排序两队玩家。
func buildResult(players []Player, maskA int) *Result {
	res := &Result{
		A: Team{Name: "天辉"},
		B: Team{Name: "夜魇"},
	}
	for i, p := range players {
		if maskA&(1<<i) != 0 {
			res.A.Players = append(res.A.Players, p)
		} else {
			res.B.Players = append(res.B.Players, p)
		}
	}
	res.ConflictA = assignTeamPositions(&res.A)
	res.ConflictB = assignTeamPositions(&res.B)
	res.refreshScores()
	return res
}

func (res *Result) refreshScores() {
	res.ScoreA, res.TierA = teamScore(res.A.Players)
	res.ScoreB, res.TierB = teamScore(res.B.Players)
	res.ScoreDiff = absInt(res.ScoreA - res.ScoreB)
	res.Cost = res.ScoreDiff + (res.ConflictA+res.ConflictB)*weightPositionConflict
}

func teamScore(players []Player) (score, tiers int) {
	for _, p := range players {
		score += p.EffMMR
		tiers += p.Tier
	}
	return score, tiers
}

// teamPositionConflict 统计单队位置冲突人次：队人数减去最大位置匹配数，
// 即无论如何安排都会有同位置重叠的人数；多位置玩家可在其可打位置间灵活消化冲突。
func teamPositionConflict(team []Player) int {
	_, matched := matchPositions(team)
	return len(team) - matched
}

func assignTeamPositions(team *Team) int {
	owners, matched := matchPositions(team.Players)
	team.Assignments = make(map[string]int, len(team.Players))
	for _, p := range team.Players {
		team.Assignments[p.Name] = 0
	}
	for pos, owner := range owners {
		if owner != 0 {
			team.Assignments[team.Players[owner-1].Name] = pos
		}
	}
	sort.Slice(team.Players, func(i, j int) bool {
		a, b := team.Players[i].Name, team.Players[j].Name
		posA, posB := team.Assignments[a], team.Assignments[b]
		if posA != posB {
			return posA != 0 && (posB == 0 || posA < posB)
		}
		return a < b
	})
	return len(team.Players) - matched
}
