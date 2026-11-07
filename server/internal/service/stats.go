package service

import (
	"context"
	"time"

	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/store"
)

type MonthSummary struct {
	Month        string
	IncomeCents  int64
	ExpenseCents int64
	Count        int
}

type CategoryStat struct {
	CategoryID  string
	Name        string
	Color       int64
	IconKey     string
	AmountCents int64
	Count       int
	Ratio       float64
}

type TrendPoint struct {
	Month       string
	AmountCents int64
}

func parseMonth(month string) (time.Time, error) {
	if month == "" {
		now := time.Now()
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local), nil
	}
	t, err := time.ParseInLocation("2006-01", month, time.Local)
	if err != nil {
		return time.Time{}, domain.ErrBadRequest.WithMessage("month 格式应为 2026-11")
	}
	return t, nil
}

func (s *LedgerService) Summary(ctx context.Context, familyID, userID, month string) (*MonthSummary, error) {
	if _, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID); err != nil {
		return nil, err
	}
	start, err := parseMonth(month)
	if err != nil {
		return nil, err
	}
	end := start.AddDate(0, 1, 0)

	sums, err := s.db.SumByType(ctx, s.db.SQL(), familyID, userID, start.UnixMilli(), end.UnixMilli())
	if err != nil {
		return nil, err
	}
	out := &MonthSummary{Month: start.Format("2006-01")}
	for _, item := range sums {
		if item.Type == domain.TypeIncome {
			out.IncomeCents = item.Amount
		} else {
			out.ExpenseCents = item.Amount
		}
		out.Count += item.Count
	}
	return out, nil
}

func (s *LedgerService) CategoryStats(ctx context.Context, familyID, userID, month string, typ int) ([]CategoryStat, error) {
	if _, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID); err != nil {
		return nil, err
	}
	if typ != domain.TypeExpense && typ != domain.TypeIncome {
		typ = domain.TypeExpense
	}
	start, err := parseMonth(month)
	if err != nil {
		return nil, err
	}
	end := start.AddDate(0, 1, 0)

	sums, err := s.db.SumByCategory(ctx, s.db.SQL(), familyID, userID, start.UnixMilli(), end.UnixMilli(), typ)
	if err != nil {
		return nil, err
	}
	categories, err := s.db.CategoriesOfFamily(ctx, s.db.SQL(), familyID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]store.Category, len(categories))
	for _, c := range categories {
		byID[c.ID] = c
	}

	var total int64
	for _, item := range sums {
		total += item.Amount
	}
	out := make([]CategoryStat, 0, len(sums))
	for _, item := range sums {
		stat := CategoryStat{
			CategoryID:  item.CategoryID,
			AmountCents: item.Amount,
			Count:       item.Count,
			Name:        "已删除",
			Color:       0xFF78909C,
			IconKey:     "other",
		}
		if c, ok := byID[item.CategoryID]; ok {
			stat.Name = c.Name
			stat.Color = c.Color
			stat.IconKey = c.IconKey
		}
		if total > 0 {
			stat.Ratio = float64(item.Amount) / float64(total)
		}
		out = append(out, stat)
	}
	return out, nil
}

// Trend 近 N 个月（含当月）的收支趋势，只统计一个口径。
func (s *LedgerService) Trend(ctx context.Context, familyID, userID string, months, typ int) ([]TrendPoint, error) {
	if _, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID); err != nil {
		return nil, err
	}
	if months <= 0 || months > 12 {
		months = 6
	}
	if typ != domain.TypeExpense && typ != domain.TypeIncome {
		typ = domain.TypeExpense
	}
	now := time.Now()
	base := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)

	out := make([]TrendPoint, 0, months)
	for i := months - 1; i >= 0; i-- {
		start := base.AddDate(0, -i, 0)
		end := start.AddDate(0, 1, 0)
		sums, err := s.db.SumByType(ctx, s.db.SQL(), familyID, userID, start.UnixMilli(), end.UnixMilli())
		if err != nil {
			return nil, err
		}
		point := TrendPoint{Month: start.Format("2006-01")}
		for _, item := range sums {
			if item.Type == typ {
				point.AmountCents = item.Amount
			}
		}
		out = append(out, point)
	}
	return out, nil
}

// ScopeStat 一种归属的汇总
type ScopeStat struct {
	Scope       string `json:"scope"`
	AmountCents int64  `json:"amount_cents"`
	Count       int    `json:"count"`
}

// OwnerStat 某个成员的个人开支
type OwnerStat struct {
	OwnerID     string `json:"owner_id"`
	AmountCents int64  `json:"amount_cents"`
	Count       int    `json:"count"`
}

// ScopeBreakdown 按「家庭/个人/资金往来/待确认」拆分支出，
// 个人部分再按成员拆开（谁花的）。
func (s *LedgerService) ScopeBreakdown(ctx context.Context, familyID, userID, month string) (map[string]any, error) {
	if _, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID); err != nil {
		return nil, err
	}
	start, err := parseMonth(month)
	if err != nil {
		return nil, err
	}
	end := start.AddDate(0, 1, 0)

	sums, err := s.db.SumByScope(ctx, s.db.SQL(), familyID, userID, start.UnixMilli(), end.UnixMilli())
	if err != nil {
		return nil, err
	}

	byScope := map[string]*ScopeStat{
		domain.ScopeFamily:   {Scope: domain.ScopeFamily},
		domain.ScopePersonal: {Scope: domain.ScopePersonal},
		domain.ScopeTransfer: {Scope: domain.ScopeTransfer},
		domain.ScopeUnknown:  {Scope: domain.ScopeUnknown},
	}
	owners := map[string]*OwnerStat{}
	var spendTotal int64

	for _, item := range sums {
		key := item.Scope
		if key == "" {
			key = domain.ScopeUnknown
		}
		stat, ok := byScope[key]
		if !ok {
			stat = &ScopeStat{Scope: key}
			byScope[key] = stat
		}
		stat.AmountCents += item.Amount
		stat.Count += item.Count

		// 资金往来和待确认不计入消费
		if key != domain.ScopeTransfer && key != domain.ScopeUnknown {
			spendTotal += item.Amount
		}
		if key == domain.ScopePersonal && item.OwnerID != "" {
			o, ok := owners[item.OwnerID]
			if !ok {
				o = &OwnerStat{OwnerID: item.OwnerID}
				owners[item.OwnerID] = o
			}
			o.AmountCents += item.Amount
			o.Count += item.Count
		}
	}

	order := []string{domain.ScopeFamily, domain.ScopePersonal, domain.ScopeTransfer, domain.ScopeUnknown}
	items := make([]ScopeStat, 0, len(order))
	for _, key := range order {
		if stat, ok := byScope[key]; ok {
			items = append(items, *stat)
		}
	}
	ownerList := make([]OwnerStat, 0, len(owners))
	for _, o := range owners {
		ownerList = append(ownerList, *o)
	}

	return map[string]any{
		"month":       start.Format("2006-01"),
		"spend_total": spendTotal,
		"items":       items,
		"owners":      ownerList,
	}, nil
}
