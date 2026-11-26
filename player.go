package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Player 是一名参赛玩家的输入信息。
type Player struct {
	Name      string `json:"name"`                // 昵称，必填且全队唯一
	Rank      string `json:"rank"`                // 段位中文名（如“传奇”）或档位数字 1-8
	Star      int    `json:"star,omitempty"`      // 星级 1-5，可选，缺省按 1 星，冠绝一世无星级
	MMR       int    `json:"mmr,omitempty"`       // 精确分数，可选；缺省时用段位基准分兜底
	Position  int    `json:"position,omitempty"`  // 主位置 1-5，兼容旧的单位置数据；新数据以 Positions 为准
	Positions []int  `json:"positions,omitempty"` // 可打位置列表 1-5，支持一人多选；归一化后至少含一个位置
	Tier      int    `json:"tier,omitempty"`      // 解析后的档位，由程序填充
	EffMMR    int    `json:"effMMR,omitempty"`    // 参与均衡的有效分数，由程序填充
}

// playerPositions 统一取玩家的可打位置：优先 Positions，空时回退旧字段 Position，
// 使未走 normalize 的历史数据与单元测试构造的玩家也能参与多位置算法。
func playerPositions(p Player) []int {
	if len(p.Positions) > 0 {
		return p.Positions
	}
	if p.Position >= 1 && p.Position <= 5 {
		return []int{p.Position}
	}
	return nil
}

// Roster 是阵容文件的顶层结构。
type Roster struct {
	Players []Player `json:"players"`
}

// LoadRoster 从 JSON 文件读取阵容并完成解析、校验与派生字段填充。
func LoadRoster(path string) (*Roster, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取阵容文件失败: %w", err)
	}
	var roster Roster
	if err := json.Unmarshal(raw, &roster); err != nil {
		return nil, fmt.Errorf("阵容文件不是合法 JSON: %w", err)
	}
	if err := roster.normalize(); err != nil {
		return nil, err
	}
	return &roster, nil
}

// normalize 校验输入合法性，并填充 Tier、EffMMR 两个派生字段。
func (r *Roster) normalize() error {
	n := len(r.Players)
	if n < 2 {
		return fmt.Errorf("至少需要 2 名玩家，当前 %d 名", n)
	}
	if n > maxRosterSize {
		return fmt.Errorf("玩家数 %d 超出上限 %d（10 人首发 + %d 人替补）", n, maxRosterSize, maxRosterSize-starterLimit)
	}
	// 报名超过 10 人时固定取前 10 人首发（偶数），因此仅 10 人以内的奇数总人数无法平分。
	if starterCount(n)%2 != 0 {
		return fmt.Errorf("当前 %d 名玩家，首发需为偶数才能平分两队，请凑成偶数或继续报名至 10 人以上", n)
	}

	seen := make(map[string]struct{}, len(r.Players))
	for i := range r.Players {
		p := &r.Players[i]
		if p.Name == "" {
			return fmt.Errorf("第 %d 个玩家缺少 name", i+1)
		}
		if _, dup := seen[p.Name]; dup {
			return fmt.Errorf("玩家昵称重复: %s", p.Name)
		}
		seen[p.Name] = struct{}{}

		// 位置支持多选：合并 Positions 与旧字段 Position，去重并校验范围，
		// 归一化后按升序写回 Positions，主位置 Position 取首个用于排序与旧数据兼容。
		posSet := make(map[int]struct{}, len(p.Positions)+1)
		for _, pos := range p.Positions {
			if pos < 1 || pos > 5 {
				return fmt.Errorf("玩家 %s 的位置 %d 非法，位置只能是 1-5", p.Name, pos)
			}
			posSet[pos] = struct{}{}
		}
		if p.Position != 0 {
			if p.Position < 1 || p.Position > 5 {
				return fmt.Errorf("玩家 %s 的 position=%d 非法，位置只能是 1-5", p.Name, p.Position)
			}
			posSet[p.Position] = struct{}{}
		}
		if len(posSet) == 0 {
			return fmt.Errorf("玩家 %s 至少需要选择一个位置（1-5）", p.Name)
		}
		positions := make([]int, 0, len(posSet))
		for pos := 1; pos <= 5; pos++ {
			if _, ok := posSet[pos]; ok {
				positions = append(positions, pos)
			}
		}
		p.Positions = positions
		p.Position = positions[0]
		if p.Star != 0 && (p.Star < 1 || p.Star > 5) {
			return fmt.Errorf("玩家 %s 的 star=%d 非法，星级只能是 1-5", p.Name, p.Star)
		}
		if p.MMR < 0 {
			return fmt.Errorf("玩家 %s 的 mmr 不能为负数", p.Name)
		}

		tier, ok := parseTier(p.Rank)
		if !ok {
			return fmt.Errorf("玩家 %s 的段位 %q 无法识别，合法值: %s", p.Name, p.Rank, rankNames())
		}
		p.Tier = tier
		if tier == immortalTier {
			p.Star = 0
		}
		p.EffMMR = effectiveMMR(tier, p.Star, p.MMR)
	}
	return nil
}

// rankNames 返回全部合法段位中文名，用于错误提示。
func rankNames() string {
	s := ""
	for i, r := range rankTable {
		if i > 0 {
			s += "、"
		}
		s += r.Name
	}
	return s
}
