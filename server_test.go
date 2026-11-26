package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestHandleBalanceOK 验证合法阵容能返回 200 与结构完整的分队结果。
func TestHandleBalanceOK(t *testing.T) {
	body := `{"players":[
		{"name":"甲","rank":"传奇","position":1,"mmr":3000},
		{"name":"乙","rank":"统帅","position":2,"mmr":2600},
		{"name":"丙","rank":"中军","position":3,"mmr":1900},
		{"name":"丁","rank":"卫士","position":4,"mmr":1000}
	],"seed":0}`
	req := httptest.NewRequest(http.MethodPost, "/api/balance", strings.NewReader(body))
	rec := httptest.NewRecorder()
	(&apiServer{}).handleBalance(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", rec.Code, rec.Body.String())
	}
	var res Result
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("响应不是合法 Result JSON: %v", err)
	}
	if len(res.A.Players) != 2 || len(res.B.Players) != 2 {
		t.Errorf("两队各应 2 人，实际 %d/%d", len(res.A.Players), len(res.B.Players))
	}
}

// TestHandleBalanceBadRequest 验证非法输入返回 400 与统一错误结构。
func TestHandleBalanceBadRequest(t *testing.T) {
	body := `{"players":[
		{"name":"甲","rank":"传奇","position":1},
		{"name":"乙","rank":"统帅","position":2},
		{"name":"丙","rank":"中军","position":3}
	]}`
	req := httptest.NewRequest(http.MethodPost, "/api/balance", strings.NewReader(body))
	rec := httptest.NewRecorder()
	(&apiServer{}).handleBalance(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("奇数人数应返回 400，实际 %d", rec.Code)
	}
	var e apiError
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || e.Error == "" {
		t.Fatalf("应返回非空 error 字段，实际 %s", rec.Body.String())
	}
}

// TestHandleBalanceMalformedJSON 验证 JSON 解析失败也走 400 而不是崩溃。
func TestHandleBalanceMalformedJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/balance", strings.NewReader("{not-json"))
	rec := httptest.NewRecorder()
	(&apiServer{}).handleBalance(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应返回 400，实际 %d", rec.Code)
	}
}

// TestHandleBalanceBench 验证 11 人报名走 HTTP 接口时返回 200，
// 前 10 人首发、第 11 人按录入顺序进入 bench。
func TestHandleBalanceBench(t *testing.T) {
	players := make([]Player, 0, 11)
	for i := 0; i < 11; i++ {
		players = append(players, mustPlayer(t,
			fmt.Sprintf("W%02d", i+1), "传奇", 0, 3000+i*10, (i%5)+1))
	}
	body, err := json.Marshal(balanceRequest{Players: players})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/balance", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	(&apiServer{}).handleBalance(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("11 人应返回 200，实际 %d: %s", rec.Code, rec.Body.String())
	}
	var res Result
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("响应不是合法 Result JSON: %v", err)
	}
	if len(res.A.Players) != 5 || len(res.B.Players) != 5 {
		t.Errorf("首发两队各应 5 人，实际 %d/%d", len(res.A.Players), len(res.B.Players))
	}
	if len(res.Bench) != 1 || res.Bench[0].Name != "W11" {
		t.Errorf("替补席应为 W11 单人，实际 %+v", res.Bench)
	}
}

// TestHistoryEndpoints 走完整 mux 验证历史接口：
// 分队自动入库 -> 列表 200 -> 详情 200 -> 删除 204 -> 再查 404。
func TestHistoryEndpoints(t *testing.T) {
	store := newTestStore(t)
	mux, err := newMux(&apiServer{store: store})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(mux)
	defer ts.Close()

	body := `{"players":[
		{"name":"甲","rank":"传奇","position":1,"mmr":3000},
		{"name":"乙","rank":"统帅","position":2,"mmr":2600},
		{"name":"丙","rank":"中军","position":3,"mmr":1900},
		{"name":"丁","rank":"卫士","position":4,"mmr":1000}
	]}`
	resp, err := http.Post(ts.URL+"/api/balance", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("分队应 200，实际 %d", resp.StatusCode)
	}
	var res Result
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if res.RecordID == 0 {
		t.Fatal("持久化模式下响应应回填 recordID")
	}

	listResp, err := http.Get(ts.URL + "/api/history")
	if err != nil {
		t.Fatal(err)
	}
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("历史列表应 200，实际 %d", listResp.StatusCode)
	}
	var list struct {
		Records []HistorySummary `json:"records"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	listResp.Body.Close()
	if len(list.Records) != 1 || list.Records[0].ID != res.RecordID {
		t.Fatalf("历史列表异常: %+v", list.Records)
	}

	detailResp, err := http.Get(ts.URL + fmt.Sprintf("/api/history/%d", res.RecordID))
	if err != nil {
		t.Fatal(err)
	}
	if detailResp.StatusCode != http.StatusOK {
		t.Fatalf("历史详情应 200，实际 %d", detailResp.StatusCode)
	}
	var detail HistoryDetail
	if err := json.NewDecoder(detailResp.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	detailResp.Body.Close()
	if len(detail.Result.A.Players) != 2 || len(detail.Request.Players) != 4 {
		t.Fatalf("详情还原异常: %+v", detail)
	}

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+fmt.Sprintf("/api/history/%d", res.RecordID), nil)
	delResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("删除应 204，实际 %d", delResp.StatusCode)
	}
	delResp.Body.Close()

	missingResp, err := http.Get(ts.URL + fmt.Sprintf("/api/history/%d", res.RecordID))
	if err != nil {
		t.Fatal(err)
	}
	if missingResp.StatusCode != http.StatusNotFound {
		t.Fatalf("删除后详情应 404，实际 %d", missingResp.StatusCode)
	}
	missingResp.Body.Close()
}

// TestHistoryOutcomeAPI 验证战果录入接口：合法值 200 并回写、非法值 400、不存在 ID 404。
func TestHistoryOutcomeAPI(t *testing.T) {
	store := newTestStore(t)
	mux, err := newMux(&apiServer{store: store})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(mux)
	defer ts.Close()

	body := `{"players":[
		{"name":"甲","rank":"传奇","positions":[1,2],"mmr":3000},
		{"name":"乙","rank":"统帅","position":2,"mmr":2600}
	]}`
	resp, err := http.Post(ts.URL+"/api/balance", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var res Result
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if res.RecordID == 0 {
		t.Fatal("应回填 recordID")
	}

	put := func(outcome string) int {
		raw := fmt.Sprintf(`{"outcome":%q}`, outcome)
		req, _ := http.NewRequest(http.MethodPut,
			fmt.Sprintf("%s/api/history/%d/outcome", ts.URL, res.RecordID), strings.NewReader(raw))
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		return r.StatusCode
	}
	if code := put("win"); code != http.StatusOK {
		t.Fatalf("录入 win 应 200，实际 %d", code)
	}
	if code := put("draw"); code != http.StatusBadRequest {
		t.Fatalf("非法 outcome 应 400，实际 %d", code)
	}

	detailResp, _ := http.Get(ts.URL + fmt.Sprintf("/api/history/%d", res.RecordID))
	var detail HistoryDetail
	if err := json.NewDecoder(detailResp.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	detailResp.Body.Close()
	if detail.Outcome != "win" {
		t.Errorf("详情战果应为 win，实际 %q", detail.Outcome)
	}
	if got := detail.Request.Players[0].Positions; len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("多位置请求应原样存档，实际 %v", got)
	}

	req, _ := http.NewRequest(http.MethodPut,
		fmt.Sprintf("%s/api/history/99999/outcome", ts.URL), strings.NewReader(`{"outcome":"win"}`))
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("不存在的记录应 404，实际 %d", r.StatusCode)
	}
}

func TestBO3ResumeAndFinish(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bo3.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	mux, err := newMux(&apiServer{store: store})
	if err != nil {
		t.Fatal(err)
	}
	send := func(method, path, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		out := httptest.NewRecorder()
		mux.ServeHTTP(out, httptest.NewRequest(method, path, strings.NewReader(body)))
		if out.Code != want {
			t.Fatalf("%s %s: %d, expected %d; %s", method, path, out.Code, want, out.Body)
		}
		return out
	}
	balance := send(http.MethodPost, "/api/balance", `{"bestOf":3,"players":[
		{"name":"甲","rank":"传奇","positions":[1,2]},
		{"name":"乙","rank":"统帅","position":2}
	]}`, http.StatusOK)
	var result Result
	if err := json.Unmarshal(balance.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/history/%d", result.RecordID)
	send(http.MethodPut, path+"/rounds", `{"previous":[],"outcome":"win"}`, http.StatusOK)
	send(http.MethodPut, path+"/rounds", `{"previous":[],"outcome":"win"}`, http.StatusConflict)
	send(http.MethodPut, path+"/rounds", `{"previous":["win"],"outcome":"lose"}`, http.StatusOK)
	resumed := send(http.MethodGet, path, "", http.StatusOK)
	var detail HistoryDetail
	if err := json.Unmarshal(resumed.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.BestOf != 3 || len(detail.Rounds) != 2 || detail.Outcome != "" ||
		len(detail.Request.Players) != 2 || detail.Result.A.Assignments["甲"] == 0 {
		t.Fatalf("未正确恢复 1:1 和原阵容: %+v", detail)
	}
	send(http.MethodPut, path+"/rounds", `{"previous":["win","lose"],"outcome":"win"}`, http.StatusOK)
	send(http.MethodPut, path+"/rounds", `{"previous":["win","lose","win"],"outcome":"lose"}`, http.StatusBadRequest)
	finished, err := store.GetRecord(result.RecordID)
	if err != nil || finished.Outcome != "win" || len(finished.Rounds) != 3 {
		t.Fatalf("应以 2:1 结束比赛: %+v (%v)", finished, err)
	}
	send(http.MethodPut, path+"/rounds", `{"previous":["win","lose","win"],"outcome":"undo"}`, http.StatusOK)
	send(http.MethodPut, path+"/rounds", `{"previous":["win","lose"],"outcome":"lose"}`, http.StatusOK)
	finished, err = store.GetRecord(result.RecordID)
	if err != nil || finished.Outcome != "lose" {
		t.Fatalf("撤销并重录后应为夜魇胜: %+v (%v)", finished, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	history, err := reopened.ListRecords(1)
	if err != nil || len(history) != 1 || history[0].BestOf != 3 ||
		len(history[0].Rounds) != 3 || history[0].Outcome != "lose" {
		t.Fatalf("重新打开数据库后应恢复完整 BO3: %+v (%v)", history, err)
	}
}

func TestBO3SubstitutionPreservesTeamsAndScore(t *testing.T) {
	store := newTestStore(t)
	mux, err := newMux(&apiServer{store: store})
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path string, body any, want int) []byte {
		t.Helper()
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		out := httptest.NewRecorder()
		mux.ServeHTTP(out, httptest.NewRequest(method, path, bytes.NewReader(payload)))
		if out.Code != want {
			t.Fatalf("%s %s: %d, expected %d; %s", method, path, out.Code, want, out.Body)
		}
		return out.Body.Bytes()
	}
	players := make([]Player, 0, 12)
	for i := 0; i < 12; i++ {
		players = append(players, Player{
			Name: fmt.Sprintf("玩家%d", i+1), Rank: "中军", MMR: 3000 + i,
			Positions: []int{1, 2, 3, 4, 5},
		})
	}
	var before Result
	if err := json.Unmarshal(request(http.MethodPost, "/api/balance", balanceRequest{Players: players, BestOf: 3}, http.StatusOK), &before); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/history/%d", before.RecordID)
	request(http.MethodPut, path+"/rounds", map[string]any{"outcome": "win"}, http.StatusOK)
	incoming, outgoing := before.Bench[1].Name, before.A.Players[0].Name
	swap := map[string]any{"incoming": incoming, "outgoing": outgoing, "rounds": []string{"win"}, "revision": 0}

	var after HistoryDetail
	if err := json.Unmarshal(request(http.MethodPut, path+"/substitution", swap, http.StatusOK), &after); err != nil {
		t.Fatal(err)
	}
	if len(after.Rounds) != 1 || after.Rounds[0] != "win" || after.Outcome != "" {
		t.Fatalf("换人后应保留 1:0: %+v", after.HistorySummary)
	}
	if after.Result.ScoreA != before.ScoreA-before.A.Players[0].EffMMR+before.Bench[1].EffMMR ||
		after.Result.ScoreB != before.ScoreB || !reflect.DeepEqual(after.Result.B, before.B) {
		t.Fatalf("只应调整换人的一方: %+v", after.Result)
	}
	if playerIndex(after.Request.Players, incoming) >= starterLimit ||
		playerIndex(after.Request.Players, outgoing) < starterLimit ||
		after.Result.Bench[1].Name != outgoing || after.Result.A.Assignments[incoming] == 0 {
		t.Fatalf("首发、替补和落位未正确更新: %+v", after.Result)
	}
	if got := after.Result.Substitutions; len(got) != 1 || got[0].FromRound != 2 || got[0].Team != before.A.Name {
		t.Fatalf("换人记录错误: %+v", got)
	}

	request(http.MethodPut, path+"/substitution", swap, http.StatusConflict)
	request(http.MethodPut, path+"/rounds", map[string]any{
		"previous": []string{"win"}, "outcome": "win", "revision": 0,
	}, http.StatusConflict)
	request(http.MethodPut, path+"/rounds", map[string]any{
		"previous": []string{"win"}, "outcome": "win", "revision": 1,
	}, http.StatusOK)
	var restored HistoryDetail
	if err := json.Unmarshal(request(http.MethodGet, path, nil, http.StatusOK), &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Outcome != "win" || !reflect.DeepEqual(restored.Result, after.Result) {
		t.Fatalf("战史应恢复换人后的阵容和最终比分: %+v", restored)
	}
}
