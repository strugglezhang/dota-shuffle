package main

import "strconv"

// Dota2 段位体系：先锋至超凡入圣各含 1-5 星，冠绝一世无星级。
// BaseMMR 是该段位一星的近似基准分，仅在玩家未填写精确 MMR 时用于兜底估算，
// 数值取整便于口算，不代表官方精确换算。
const starStepMMR = 140
const immortalTier = 8

// RankInfo 描述一个段位档位。
type RankInfo struct {
	Tier    int    // 档位序号 1-8
	Name    string // 中文段位名
	BaseMMR int    // 该档位一星的近似基准分
}

// rankTable 按档位升序排列，索引即 Tier-1。
var rankTable = []RankInfo{
	{1, "先锋", 0},
	{2, "卫士", 700},
	{3, "中军", 1400},
	{4, "统帅", 2100},
	{5, "传奇", 2800},
	{6, "万古流芳", 3500},
	{7, "超凡入圣", 4200},
	{8, "冠绝一世", 5000},
}

// rankByTier 按档位序号取段位信息，越界返回 false。
func rankByTier(tier int) (RankInfo, bool) {
	if tier < 1 || tier > len(rankTable) {
		return RankInfo{}, false
	}
	return rankTable[tier-1], true
}

// parseTier 把玩家填写的段位解析为档位序号。
// 优先按中文名匹配（如“传奇”），也接受纯数字档位（如“5”）。
func parseTier(rank string) (int, bool) {
	for _, r := range rankTable {
		if r.Name == rank {
			return r.Tier, true
		}
	}
	var n int
	if _, err := strconv.Atoi(rank); err == nil {
		n, _ = strconv.Atoi(rank)
		if _, ok := rankByTier(n); ok {
			return n, true
		}
	}
	return 0, false
}

// tierBaseMMR 返回某档位一星的基准分。
func tierBaseMMR(tier int) int {
	info, ok := rankByTier(tier)
	if !ok {
		return 0
	}
	return info.BaseMMR
}

// tierName 返回档位中文名，未知档位返回空串。
func tierName(tier int) string {
	info, ok := rankByTier(tier)
	if !ok {
		return ""
	}
	return info.Name
}

// effectiveMMR 计算玩家参与均衡的有效分数：
// 填写了精确 MMR 时以 MMR 为准，否则用“段位基准分 + 星级增量”兜底。
func effectiveMMR(tier, star, mmr int) int {
	if mmr > 0 {
		return mmr
	}
	if star < 1 || tier == immortalTier {
		star = 1
	}
	return tierBaseMMR(tier) + (star-1)*starStepMMR
}
