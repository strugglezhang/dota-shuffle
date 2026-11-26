package main

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"
)

// mustPlayer 快速构造一个已归一化的玩家，供算法测试使用。
func mustPlayer(t *testing.T, name, rank string, star, mmr, pos int) Player {
	t.Helper()
	tier, ok := parseTier(rank)
	if !ok {
		t.Fatalf("测试段位非法: %s", rank)
	}
	return Player{
		Name:     name,
		Rank:     rank,
		Star:     star,
		MMR:      mmr,
		Position: pos,
		Tier:     tier,
		EffMMR:   effectiveMMR(tier, star, mmr),
	}
}

func TestParseTier(t *testing.T) {
	cases := map[string]int{
		"先锋": 1, "冠绝一世": 8, "传奇": 5, "3": 3, "8": 8,
	}
	for in, want := range cases {
		got, ok := parseTier(in)
		if !ok || got != want {
			t.Errorf("parseTier(%q) = (%d,%v), want %d", in, got, ok, want)
		}
	}
	if _, ok := parseTier("不存在的段位"); ok {
		t.Error("非法段位应当解析失败")
	}
}

func TestEffectiveMMR(t *testing.T) {
	// 填了精确 MMR 时一律以 MMR 为准。
	if got := effectiveMMR(5, 3, 3300); got != 3300 {
		t.Errorf("精确 MMR 未生效: got %d", got)
	}
	// 未填 MMR：传奇(2800) 3 星 = 2800 + 2*140。
	if got := effectiveMMR(5, 3, 0); got != 3080 {
		t.Errorf("段位兜底分数错误: got %d want 3080", got)
	}
	// 星级缺省按 1 星，不加增量。
	if got := effectiveMMR(5, 0, 0); got != 2800 {
		t.Errorf("缺省星级计算错误: got %d want 2800", got)
	}
	if got := effectiveMMR(immortalTier, 5, 0); got != tierBaseMMR(immortalTier) {
		t.Errorf("冠绝一世的估算不应叠加旧星级，实际 %d", got)
	}
}

func TestNormalizeErrors(t *testing.T) {
	good := []Player{
		mustPlayer(t, "甲", "传奇", 0, 3000, 1),
		mustPlayer(t, "乙", "统帅", 0, 2900, 2),
	}

	t.Run("奇数人数", func(t *testing.T) {
		r := &Roster{Players: append(append([]Player{}, good...),
			mustPlayer(t, "丙", "卫士", 0, 1000, 3))}
		if err := r.normalize(); err == nil {
			t.Error("奇数人数应当报错")
		}
	})
	t.Run("位置越界", func(t *testing.T) {
		bad := good[1]
		bad.Position = 6
		r := &Roster{Players: []Player{good[0], bad}}
		if err := r.normalize(); err == nil {
			t.Error("position=6 应当报错")
		}
	})
	t.Run("昵称重复", func(t *testing.T) {
		dup := good[1]
		dup.Name = "甲"
		r := &Roster{Players: []Player{good[0], dup}}
		if err := r.normalize(); err == nil {
			t.Error("重名应当报错")
		}
	})
	t.Run("段位非法", func(t *testing.T) {
		r := &Roster{Players: []Player{
			{Name: "甲", Rank: "传奇", Position: 1},
			{Name: "乙", Rank: "火星段", Position: 2},
		}}
		if err := r.normalize(); err == nil {
			t.Error("非法段位应当报错")
		}
	})
	t.Run("负分数", func(t *testing.T) {
		bad := good[0]
		bad.MMR = -1
		r := &Roster{Players: []Player{bad, good[1]}}
		if err := r.normalize(); err == nil {
			t.Error("负 MMR 应当报错")
		}
	})
}

// TestBalancePerfectTen 验证标准 10 人阵容：每位置恰好两人，
// 最优解必须位置零冲突，且两队总分守恒、分差被压到很小。
func TestBalancePerfectTen(t *testing.T) {
	// 每个位置两人，分数成对。
	players := []Player{
		mustPlayer(t, "C1", "传奇", 0, 3000, 1),
		mustPlayer(t, "C2", "传奇", 0, 3100, 1),
		mustPlayer(t, "M1", "传奇", 0, 2900, 2),
		mustPlayer(t, "M2", "传奇", 0, 3000, 2),
		mustPlayer(t, "O1", "统帅", 0, 2800, 3),
		mustPlayer(t, "O2", "统帅", 0, 2900, 3),
		mustPlayer(t, "S4a", "统帅", 0, 2700, 4),
		mustPlayer(t, "S4b", "统帅", 0, 2800, 4),
		mustPlayer(t, "S5a", "中军", 0, 2600, 5),
		mustPlayer(t, "S5b", "中军", 0, 2600, 5),
	}
	res, err := Balance(players, 0)
	if err != nil {
		t.Fatalf("Balance 返回错误: %v", err)
	}
	if len(res.A.Players) != 5 || len(res.B.Players) != 5 {
		t.Fatalf("每队应为 5 人，实际 %d/%d", len(res.A.Players), len(res.B.Players))
	}
	if res.ConflictA != 0 || res.ConflictB != 0 {
		t.Errorf("标准阵容应零位置冲突，实际 %d/%d", res.ConflictA, res.ConflictB)
	}
	// 总分守恒。
	var total int
	for _, p := range players {
		total += p.EffMMR
	}
	if res.ScoreA+res.ScoreB != total {
		t.Errorf("总分不守恒: %d+%d != %d", res.ScoreA, res.ScoreB, total)
	}
	// 成对分数可完全配平，理论最小分差为 0。
	if res.ScoreDiff != 0 {
		t.Errorf("可配平阵容期望分差 0，实际 %d", res.ScoreDiff)
	}
}

// TestBalancePositionPriority 验证位置冲突的优先级高于分数：
// 6 个一号位 unavoidable 冲突时，算法仍应找到最均匀的 3/3 分布。
func TestBalancePositionPriority(t *testing.T) {
	players := []Player{
		mustPlayer(t, "C1", "冠绝一世", 0, 6000, 1),
		mustPlayer(t, "C2", "冠绝一世", 0, 5900, 1),
		mustPlayer(t, "C3", "超凡入圣", 0, 4800, 1),
		mustPlayer(t, "C4", "超凡入圣", 0, 4700, 1),
		mustPlayer(t, "C5", "传奇", 0, 3300, 1),
		mustPlayer(t, "C6", "传奇", 0, 3200, 1),
		mustPlayer(t, "M", "统帅", 0, 2500, 2),
		mustPlayer(t, "O", "统帅", 0, 2400, 3),
		mustPlayer(t, "S4", "中军", 0, 1800, 4),
		mustPlayer(t, "S5", "中军", 0, 1700, 5),
	}
	res, err := Balance(players, 0)
	if err != nil {
		t.Fatalf("Balance 返回错误: %v", err)
	}
	// 6 个一号位分到两队，最均匀为 3/3，每队冲突 2，合计 4。
	if got := res.ConflictA + res.ConflictB; got != 4 {
		t.Errorf("位置冲突应取理论最小值 4，实际 %d（%d/%d）",
			got, res.ConflictA, res.ConflictB)
	}
}

func TestBalanceDeterministic(t *testing.T) {
	players := []Player{
		mustPlayer(t, "甲", "传奇", 2, 0, 1),
		mustPlayer(t, "乙", "统帅", 0, 2500, 2),
		mustPlayer(t, "丙", "中军", 0, 1800, 3),
		mustPlayer(t, "丁", "卫士", 0, 900, 4),
	}
	r1, err := Balance(players, 0)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Balance(players, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r1, r2) {
		t.Error("seed=0 时两次分队结果应当完全一致")
	}
}

func TestBalanceSeedReproducible(t *testing.T) {
	players := []Player{
		mustPlayer(t, "甲", "传奇", 0, 3000, 1),
		mustPlayer(t, "乙", "传奇", 0, 3050, 1),
		mustPlayer(t, "丙", "统帅", 0, 2600, 2),
		mustPlayer(t, "丁", "统帅", 0, 2650, 2),
		mustPlayer(t, "戊", "中军", 0, 1900, 3),
		mustPlayer(t, "己", "中军", 0, 1950, 3),
		mustPlayer(t, "庚", "卫士", 0, 1000, 4),
		mustPlayer(t, "辛", "卫士", 0, 1050, 4),
		mustPlayer(t, "壬", "先锋", 0, 300, 5),
		mustPlayer(t, "癸", "先锋", 0, 350, 5),
	}
	r1, _ := Balance(players, 42)
	r2, _ := Balance(players, 42)
	if !reflect.DeepEqual(r1, r2) {
		t.Error("相同 seed 应当产生可复现的相同结果")
	}
}

func TestBalanceBadSize(t *testing.T) {
	one := []Player{mustPlayer(t, " solo", "传奇", 0, 3000, 1)}
	if _, err := Balance(one, 0); err == nil {
		t.Error("单人无法分队，应当报错")
	}
}

// TestNormalizeBenchAllowed 验证替补感知校验：
// 11/12 人（首发固定 10 人）合法，9 人首发为奇数仍被拒绝。
func TestNormalizeBenchAllowed(t *testing.T) {
	makeN := func(n int) []Player {
		out := make([]Player, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, mustPlayer(t, fmt.Sprintf("Q%02d", i+1), "传奇", 0, 3000, (i%5)+1))
		}
		return out
	}
	for _, n := range []int{11, 12, 20} {
		if err := (&Roster{Players: makeN(n)}).normalize(); err != nil {
			t.Errorf("%d 人（首发 %d + 替补 %d）应合法，实际报错: %v", n, starterCount(n), n-starterCount(n), err)
		}
	}
	if err := (&Roster{Players: makeN(9)}).normalize(); err == nil {
		t.Error("9 人首发为奇数无法平分，应当报错")
	}
	if err := (&Roster{Players: makeN(21)}).normalize(); err == nil {
		t.Error("21 人超过上限 20，应当报错")
	}
}

// TestBalanceBenchByOrder 验证超过 10 人时按录入顺序切分首发/替补：
// 前 10 人进入两队各 5 人，第 11 人起原样进入替补席且不参与均衡。
func TestBalanceBenchByOrder(t *testing.T) {
	ranks := []string{"先锋", "卫士", "中军", "统帅", "传奇", "万古流芳", "超凡入圣", "冠绝一世"}
	players := make([]Player, 0, 12)
	for i := 0; i < 12; i++ {
		players = append(players, mustPlayer(t,
			fmt.Sprintf("P%02d", i+1), ranks[i%len(ranks)], 0, 2000+i*100, (i%5)+1))
	}

	res, err := Balance(players, 0)
	if err != nil {
		t.Fatalf("Balance 返回错误: %v", err)
	}
	if len(res.A.Players) != 5 || len(res.B.Players) != 5 {
		t.Fatalf("首发两队各应 5 人，实际 %d/%d", len(res.A.Players), len(res.B.Players))
	}
	if len(res.Bench) != 2 {
		t.Fatalf("替补应有 2 人，实际 %d", len(res.Bench))
	}
	if res.Bench[0].Name != "P11" || res.Bench[1].Name != "P12" {
		t.Errorf("替补应按录入顺序为 P11/P12，实际 %s/%s", res.Bench[0].Name, res.Bench[1].Name)
	}

	starters := map[string]bool{}
	for _, p := range append(append([]Player{}, res.A.Players...), res.B.Players...) {
		starters[p.Name] = true
	}
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("P%02d", i+1)
		if !starters[name] {
			t.Errorf("首发缺少 %s", name)
		}
	}
	for _, b := range res.Bench {
		if starters[b.Name] {
			t.Errorf("替补 %s 不应出现在首发队伍中", b.Name)
		}
	}

	// 总分守恒只统计首发 10 人，替补分数不计入两队。
	var starterTotal int
	for i := 0; i < 10; i++ {
		starterTotal += players[i].EffMMR
	}
	if res.ScoreA+res.ScoreB != starterTotal {
		t.Errorf("首发总分不守恒: %d+%d != %d", res.ScoreA, res.ScoreB, starterTotal)
	}
}

func TestPopCount(t *testing.T) {
	cases := map[int]int{0: 0, 1: 1, 3: 2, 255: 8, 256: 1}
	for in, want := range cases {
		if got := popCount(in); got != want {
			t.Errorf("popCount(%d) = %d, want %d", in, got, want)
		}
	}
}

// TestNormalizeMultiPositions 验证一人多位置：合并去重、升序归一化、
// 主位置回填，以及无位置时报错、旧单位置数据自动回退。
func TestNormalizeMultiPositions(t *testing.T) {
	r := &Roster{Players: []Player{
		{Name: "甲", Rank: "传奇", Positions: []int{3, 1, 3}, MMR: 3000},
		{Name: "乙", Rank: "统帅", Position: 2, MMR: 2600},
	}}
	if err := r.normalize(); err != nil {
		t.Fatalf("多位置归一化失败: %v", err)
	}
	if got := r.Players[0].Positions; !reflect.DeepEqual(got, []int{1, 3}) {
		t.Errorf("多位置应去重并升序，实际 %v", got)
	}
	if r.Players[0].Position != 1 {
		t.Errorf("主位置应回填为首个位置 1，实际 %d", r.Players[0].Position)
	}
	if got := r.Players[1].Positions; !reflect.DeepEqual(got, []int{2}) {
		t.Errorf("旧单位置数据应回退为 [2]，实际 %v", got)
	}

	missing := &Roster{Players: []Player{
		{Name: "甲", Rank: "传奇", MMR: 3000},
		{Name: "乙", Rank: "统帅", MMR: 2600},
	}}
	if err := missing.normalize(); err == nil {
		t.Error("未选任何位置应当报错")
	}

	badRange := &Roster{Players: []Player{
		{Name: "甲", Rank: "传奇", Positions: []int{1, 6}, MMR: 3000},
		{Name: "乙", Rank: "统帅", Position: 2, MMR: 2600},
	}}
	if err := badRange.normalize(); err == nil {
		t.Error("位置 6 越界应当报错")
	}
}

// TestBalanceMultiPositionFlex 验证多位置玩家可通过二分图匹配消化冲突：
// 两个摇摆人能打 1/2 号位，与两个纯一号位混搭时，最优解应做到零冲突。
func TestBalanceMultiPositionFlex(t *testing.T) {
	mk := func(name string, mmr int, positions ...int) Player {
		tier, _ := parseTier("传奇")
		return Player{Name: name, Rank: "传奇", Positions: positions, Position: positions[0],
			Tier: tier, EffMMR: effectiveMMR(tier, 0, mmr)}
	}
	players := []Player{
		mk("摇摆A", 3000, 1, 2),
		mk("摇摆B", 3000, 1, 2),
		mk("纯C1", 3000, 1),
		mk("纯C2", 3000, 1),
	}
	res, err := Balance(players, 0)
	if err != nil {
		t.Fatalf("Balance 返回错误: %v", err)
	}
	if got := res.ConflictA + res.ConflictB; got != 0 {
		t.Errorf("多位置摇摆人应把冲突消化为 0，实际 %d（%d/%d）", got, res.ConflictA, res.ConflictB)
	}

	// 对照组：四人全是纯一号位时，每队必有 1 个冲突，合计 2。
	hard := []Player{
		mk("C1", 3000, 1), mk("C2", 3000, 1), mk("C3", 3000, 1), mk("C4", 3000, 1),
	}
	res2, err := Balance(hard, 0)
	if err != nil {
		t.Fatalf("Balance 返回错误: %v", err)
	}
	if got := res2.ConflictA + res2.ConflictB; got != 2 {
		t.Errorf("纯单位置冲突应为 2，实际 %d", got)
	}
}

func TestRenderText(t *testing.T) {
	players := []Player{
		mustPlayer(t, "甲", "传奇", 0, 3000, 1),
		mustPlayer(t, "乙", "统帅", 0, 2600, 2),
		mustPlayer(t, "丙", "中军", 0, 1900, 3),
		mustPlayer(t, "丁", "卫士", 0, 1000, 4),
	}
	res, err := Balance(players, 0)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	RenderText(&buf, res, 0)
	if buf.Len() == 0 || !bytes.Contains(buf.Bytes(), []byte("天辉")) ||
		!bytes.Contains(buf.Bytes(), []byte("夜魇")) {
		t.Error("文本输出缺少队伍信息")
	}
}

func TestBalanceStrictPositionPriority(t *testing.T) {
	players := []Player{
		mustPlayer(t, "A", "传奇", 0, 300001, 1),
		mustPlayer(t, "B", "传奇", 0, 1, 1),
		mustPlayer(t, "C", "传奇", 0, 150001, 2),
		mustPlayer(t, "D", "传奇", 0, 150001, 2),
	}
	result, err := Balance(players, 42)
	if err != nil {
		t.Fatal(err)
	}
	if result.ScoreDiff != 300000 || result.ConflictA+result.ConflictB != 0 {
		t.Fatalf("应优先选择零冲突的阵容，实际分差 %d、冲突 %d/%d",
			result.ScoreDiff, result.ConflictA, result.ConflictB)
	}
}

func TestAssignedPositionsFollowPlayerChoices(t *testing.T) {
	team := Team{Players: []Player{
		{Name: "灵活", Positions: []int{1, 2}},
		{Name: "核心", Positions: []int{1}},
		{Name: "补位", Positions: []int{2}},
	}}
	if conflicts := assignTeamPositions(&team); conflicts != 1 {
		t.Fatalf("期望一人待补位，实际 %d", conflicts)
	}
	want := map[string]int{"灵活": 2, "核心": 1, "补位": 0}
	if !reflect.DeepEqual(team.Assignments, want) {
		t.Fatalf("位置分配 %v，期望 %v", team.Assignments, want)
	}
	if team.Players[0].Name != "核心" || team.Players[2].Name != "补位" {
		t.Fatalf("应按实际落位排序，待补位排末尾：%v", team.Players)
	}
}
