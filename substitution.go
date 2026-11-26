package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
)

func (s *apiServer) handleSubstitution(w http.ResponseWriter, r *http.Request) {
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
		Incoming string   `json:"incoming"`
		Outgoing string   `json:"outgoing"`
		Rounds   []string `json:"rounds"`
		Revision int      `json:"revision"`
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
	if detail.Outcome != "" || !slices.Equal(req.Rounds, detail.Rounds) || req.Revision != len(detail.Result.Substitutions) {
		writeAPIError(w, http.StatusConflict, "BO3 已更新，请重新打开后再换人")
		return
	}
	if err := detail.substitute(req.Incoming, req.Outgoing); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := s.store.saveSubstitution(detail)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "保存换人失败")
		return
	}
	if !saved {
		writeAPIError(w, http.StatusConflict, "BO3 已更新，请重新打开后再换人")
		return
	}
	detail.ScoreA, detail.ScoreB = detail.Result.ScoreA, detail.Result.ScoreB
	detail.PositionConflicts = detail.Result.ConflictA + detail.Result.ConflictB
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(detail)
}

func (detail *HistoryDetail) substitute(incoming, outgoing string) error {
	players := detail.Request.Players
	cut := starterCount(len(players))
	next, prior := playerIndex(players, incoming), playerIndex(players, outgoing)
	result := &detail.Result
	bench := playerIndex(result.Bench, incoming)
	if next < cut || prior < 0 || prior >= cut || bench < 0 {
		return errors.New("请选择一名替补和一名首发")
	}
	team, conflicts := &result.A, &result.ConflictA
	index := playerIndex(team.Players, outgoing)
	if index < 0 {
		team, conflicts = &result.B, &result.ConflictB
		index = playerIndex(team.Players, outgoing)
	}
	if index < 0 {
		return errors.New("找不到要换下的首发")
	}
	replacement := result.Bench[bench]
	if replacement.EffMMR < 0 || replacement.EffMMR > maxStarterMMR {
		return errors.New("替补的有效分数超出可计算范围")
	}
	players[next], players[prior] = players[prior], players[next]
	team.Players[index], result.Bench[bench] = replacement, team.Players[index]
	*conflicts = assignTeamPositions(team)
	result.refreshScores()
	result.Substitutions = append(result.Substitutions, Substitution{
		Incoming: incoming, Outgoing: outgoing, Team: team.Name, FromRound: len(detail.Rounds) + 1,
	})
	return nil
}

func playerIndex(players []Player, name string) int {
	return slices.IndexFunc(players, func(p Player) bool { return p.Name == name })
}
