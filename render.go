package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// positionListText 把玩家可打的多个位置渲染成紧凑文本，如 "1/3 号位"；
// 单位置玩家仍显示为 "1 号位"，与旧版输出保持一致。
func positionListText(p Player) string {
	positions := playerPositions(p)
	parts := make([]string, 0, len(positions))
	for _, pos := range positions {
		parts = append(parts, fmt.Sprintf("%d", pos))
	}
	return strings.Join(parts, "/") + " 号位"
}

// RenderText 以终端表格形式输出分队结果。
func RenderText(w io.Writer, res *Result, seed int64) {
	teamSize := len(res.A.Players)
	total := teamSize*2 + len(res.Bench)
	fmt.Fprintf(w, "=== Dota2 均衡分队结果 ===\n")
	fmt.Fprintf(w, "报名 %d 人 · 首发 %d 人（每队 %d）· 替补 %d 人 · 随机种子 %d%s\n\n",
		total, teamSize*2, teamSize, len(res.Bench), seed, seedModeHint(seed))

	renderTeam(w, res.A, res.ScoreA, res.TierA, res.ConflictA)
	fmt.Fprintln(w)
	renderTeam(w, res.B, res.ScoreB, res.TierB, res.ConflictB)

	if len(res.Bench) > 0 {
		fmt.Fprintln(w)
		renderBench(w, res.Bench)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "=== 均衡指标 ===")
	fmt.Fprintf(w, "总分差: %d · 位置冲突: 天辉 %d / 夜魇 %d\n",
		res.ScoreDiff, res.ConflictA, res.ConflictB)
	if res.ConflictA+res.ConflictB > 0 {
		fmt.Fprintln(w, "提示: 存在位置冲突，说明输入阵容里有玩家无法分配到独立的可打位置，可补位或调整后重新分队。")
	}
}

// renderBench 输出替补席，按录入顺序排列，不参与首发均衡。
func renderBench(w io.Writer, bench []Player) {
	fmt.Fprintf(w, "【替补席】共 %d 人（报名超出 10 人，按录入顺序顺延）\n", len(bench))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "位置\t玩家\t段位\t星级\t有效分数")
	for _, p := range bench {
		star := "-"
		if p.Star > 0 {
			star = fmt.Sprintf("%d 星", p.Star)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\n",
			positionListText(p), p.Name, tierName(p.Tier), star, p.EffMMR)
	}
	tw.Flush()
}

// seedModeHint 解释种子含义，便于使用者理解结果是否可复现。
func seedModeHint(seed int64) string {
	if seed > 0 {
		return "（同种子可复现的随机摇人）"
	}
	return "（确定性最优，重复执行结果一致）"
}

// renderTeam 输出单队表格与汇总信息。
func renderTeam(w io.Writer, team Team, score, tierSum, conflict int) {
	size := len(team.Players)
	avgScore := 0
	avgTier := 0.0
	if size > 0 {
		avgScore = score / size
		avgTier = float64(tierSum) / float64(size)
	}
	fmt.Fprintf(w, "【%s】总分 %d · 平均分 %d · 平均段位 %.2f（%s档）· 位置冲突 %d\n",
		team.Name, score, avgScore, avgTier, nearestTierName(avgTier), conflict)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "分配\t可打位置\t玩家\t段位\t星级\t有效分数")
	for _, p := range team.Players {
		star := "-"
		if p.Star > 0 {
			star = fmt.Sprintf("%d 星", p.Star)
		}
		assigned := "待补位"
		if position := team.Assignments[p.Name]; position > 0 {
			assigned = fmt.Sprintf("%d 号位", position)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\n",
			assigned, positionListText(p), p.Name, tierName(p.Tier), star, p.EffMMR)
	}
	tw.Flush()
}

// nearestTierName 把浮点平均档位映射到最接近的段位中文名。
func nearestTierName(avg float64) string {
	tier := int(avg + 0.5)
	if name := tierName(tier); name != "" {
		return name
	}
	return "未知"
}

// RenderJSON 输出机器可读的 JSON 结果，便于接入其他工具或网页。
func RenderJSON(w io.Writer, res *Result) error {
	buf, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return err
	}
	_, err = io.Copy(w, strings.NewReader(string(buf)+"\n"))
	return err
}
