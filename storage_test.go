package main

import (
	"path/filepath"
	"testing"
)

// newTestStore 在临时目录打开一个空数据库，测试结束自动清理。
func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test-history.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestStoreSaveListGetDelete 覆盖历史记录的完整生命周期：
// 写入两条 -> 列表倒序 -> 详情可还原请求与结果 -> 删除后查不到。
func TestStoreSaveListGetDelete(t *testing.T) {
	store := newTestStore(t)

	players := []Player{
		mustPlayer(t, "甲", "传奇", 0, 3000, 1),
		mustPlayer(t, "乙", "统帅", 0, 2600, 2),
		mustPlayer(t, "丙", "中军", 0, 1900, 3),
		mustPlayer(t, "丁", "卫士", 0, 1000, 4),
	}
	res1, err := Balance(players, 0)
	if err != nil {
		t.Fatalf("Balance 失败: %v", err)
	}
	req1 := &balanceRequest{Players: players}
	id1, err := store.SaveRecord(req1, res1)
	if err != nil {
		t.Fatalf("保存第一条记录失败: %v", err)
	}

	res2, err := Balance(players, 42)
	if err != nil {
		t.Fatalf("Balance(seed=42) 失败: %v", err)
	}
	id2, err := store.SaveRecord(&balanceRequest{Players: players, Seed: 42}, res2)
	if err != nil {
		t.Fatalf("保存第二条记录失败: %v", err)
	}
	if id1 == 0 || id2 <= id1 {
		t.Fatalf("自增 ID 异常: id1=%d id2=%d", id1, id2)
	}

	list, err := store.ListRecords(10)
	if err != nil {
		t.Fatalf("查询历史列表失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("应有 2 条历史，实际 %d", len(list))
	}
	// 列表按 ID 倒序，最新一条在前。
	if list[0].ID != id2 || list[0].Seed != 42 || list[0].RosterCount != 4 || list[0].StarterCount != 4 {
		t.Errorf("列表首行汇总字段异常: %+v", list[0])
	}
	if list[0].ScoreA != res2.ScoreA || list[0].ScoreB != res2.ScoreB {
		t.Errorf("列表分数与结果不一致: %+v", list[0])
	}

	detail, err := store.GetRecord(id1)
	if err != nil {
		t.Fatalf("查询详情失败: %v", err)
	}
	if detail == nil {
		t.Fatal("详情不应为空")
	}
	if len(detail.Request.Players) != 4 || detail.Request.Players[0].Name != "甲" {
		t.Errorf("详情中的请求阵容未正确还原: %+v", detail.Request.Players)
	}
	if len(detail.Result.A.Players) != 2 || detail.Result.ScoreDiff != res1.ScoreDiff {
		t.Errorf("详情中的分队结果未正确还原: %+v", detail.Result)
	}

	missing, err := store.GetRecord(99999)
	if err != nil {
		t.Fatalf("查询不存在的记录不应报错: %v", err)
	}
	if missing != nil {
		t.Error("不存在的 ID 应返回 nil")
	}

	ok, err := store.DeleteRecord(id1)
	if err != nil || !ok {
		t.Fatalf("删除应成功，ok=%v err=%v", ok, err)
	}
	list, _ = store.ListRecords(10)
	if len(list) != 1 || list[0].ID != id2 {
		t.Errorf("删除后应只剩 id2，实际 %+v", list)
	}
	ok, err = store.DeleteRecord(id1)
	if err != nil || ok {
		t.Errorf("重复删除应返回 false，ok=%v err=%v", ok, err)
	}
}

// TestStoreOutcome 验证战果录入、更新与清除，且列表/详情能读到最新值。
func TestStoreOutcome(t *testing.T) {
	store := newTestStore(t)
	players := []Player{
		mustPlayer(t, "甲", "传奇", 0, 3000, 1),
		mustPlayer(t, "乙", "统帅", 0, 2600, 2),
	}
	res, _ := Balance(players, 0)
	id, err := store.SaveRecord(&balanceRequest{Players: players}, res)
	if err != nil {
		t.Fatalf("保存记录失败: %v", err)
	}

	ok, err := store.SetOutcome(id, "win")
	if err != nil || !ok {
		t.Fatalf("录入天辉胜应成功，ok=%v err=%v", ok, err)
	}
	list, _ := store.ListRecords(10)
	if list[0].Outcome != "win" {
		t.Errorf("列表战果应为 win，实际 %q", list[0].Outcome)
	}
	detail, _ := store.GetRecord(id)
	if detail.Outcome != "win" {
		t.Errorf("详情战果应为 win，实际 %q", detail.Outcome)
	}

	if ok, _ := store.SetOutcome(id, "lose"); !ok {
		t.Error("更新为夜魇胜应成功")
	}
	detail, _ = store.GetRecord(id)
	if detail.Outcome != "lose" {
		t.Errorf("更新后果应为 lose，实际 %q", detail.Outcome)
	}
	if ok, _ := store.SetOutcome(99999, "win"); ok {
		t.Error("不存在的 ID 应返回 false")
	}
}

// TestStoreReopen 验证数据落盘：重新打开同一数据库后历史仍在。
func TestStoreReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	players := []Player{
		mustPlayer(t, "甲", "传奇", 0, 3000, 1),
		mustPlayer(t, "乙", "统帅", 0, 2600, 2),
	}
	res, _ := Balance(players, 0)
	if _, err := store.SaveRecord(&balanceRequest{Players: players}, res); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenStore(path)
	if err != nil {
		t.Fatalf("重新打开数据库失败: %v", err)
	}
	defer reopened.Close()
	list, err := reopened.ListRecords(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].RosterCount != 2 {
		t.Fatalf("重开后历史丢失或字段异常: %+v", list)
	}
}
