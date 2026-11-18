// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package privacy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hpk.local/hpk/internal/project"
)

// Price is what a model costs in USD per million tokens.
type Price struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`
}

// Prices come from the models.dev cache Hermes already keeps
// (<home>/models_dev_cache.json, several MB). hpk extracts the prices once into
// <home>/hpk/prices.json and rebuilds it when Hermes refreshes its cache, so a
// hook call reads a small file.
type priceBook struct {
	Source  time.Time        `json:"source"`
	Prices  map[string]Price `json:"prices"`  // "provider/model" and "model"
	Windows map[string]int   `json:"windows"` // context window in tokens, same keys
}

func loadPrices(home string) map[string]Price { return loadBook(home).Prices }

func loadBook(home string) priceBook {
	src := filepath.Join(home, "models_dev_cache.json")
	fi, err := os.Stat(src)
	if err != nil {
		return priceBook{}
	}
	book := filepath.Join(home, "hpk", "prices.json")
	var b priceBook
	// A book from before windows were kept has none: rebuild it.
	if raw, err := os.ReadFile(book); err == nil && json.Unmarshal(raw, &b) == nil && b.Source.Equal(fi.ModTime().UTC()) && b.Windows != nil {
		return b
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		return priceBook{}
	}
	var cache map[string]struct {
		Models map[string]struct {
			Cost  *Price `json:"cost"`
			Limit struct {
				Context int `json:"context"`
			} `json:"limit"`
		} `json:"models"`
	}
	if json.Unmarshal(raw, &cache) != nil {
		return priceBook{}
	}
	b = priceBook{Source: fi.ModTime().UTC(), Prices: map[string]Price{}, Windows: map[string]int{}}
	for prov, c := range cache {
		for id, m := range c.Models {
			if m.Limit.Context > 0 {
				b.Windows[prov+"/"+id] = m.Limit.Context
				if _, ok := b.Windows[id]; !ok {
					b.Windows[id] = m.Limit.Context
				}
			}
			if m.Cost == nil {
				continue
			}
			b.Prices[prov+"/"+id] = *m.Cost
			if _, ok := b.Prices[id]; !ok {
				b.Prices[id] = *m.Cost
			}
		}
	}
	if out, err := json.Marshal(b); err == nil {
		_ = os.MkdirAll(filepath.Dir(book), 0o755)
		_ = os.WriteFile(book+".tmp", out, 0o644)
		_ = os.Rename(book+".tmp", book)
	}
	return b
}

// WindowOf finds a model's context window in tokens, looked up like PriceOf.
func WindowOf(home, provider, model string) (int, bool) {
	w := loadBook(home).Windows
	if w == nil || model == "" {
		return 0, false
	}
	for _, k := range []string{provider + "/" + model, model, model[strings.LastIndex(model, "/")+1:]} {
		if n, ok := w[k]; ok {
			return n, true
		}
	}
	return 0, false
}

// PriceOf finds a model's price: provider/model first, then the model id alone
// (with and without a vendor prefix such as "anthropic/").
func PriceOf(home, provider, model string) (Price, bool) {
	prices := loadPrices(home)
	if prices == nil || model == "" {
		return Price{}, false
	}
	for _, k := range []string{provider + "/" + model, model, model[strings.LastIndex(model, "/")+1:]} {
		if p, ok := prices[k]; ok {
			return p, true
		}
	}
	return Price{}, false
}

// cost of one request's usage, in USD.
func (p Price) cost(in, out, cacheRead, cacheWrite int64) float64 {
	cr, cw := p.CacheRead, p.CacheWrite
	if cr == 0 {
		cr = p.Input
	}
	if cw == 0 {
		cw = p.Input
	}
	return (float64(in)*p.Input + float64(out)*p.Output + float64(cacheRead)*cr + float64(cacheWrite)*cw) / 1e6
}

// Spent is what a project's linked sessions (Console chats and cron runs) cost
// since a time, in USD, from the usage hpk's hooks recorded. A session counts
// whole on the day of its last request. Sessions hpk never saw are not in it.
func Spent(home string, p *project.Project, since time.Time) float64 {
	total := 0.0
	for _, id := range project.Sessions(home, p.Name) {
		if s, err := LoadSeen(home, id); err == nil && s != nil && !s.At.Before(since) {
			total += s.Cost
		}
	}
	return total
}

// Caps are a project's money limits (project.yaml budget.money_daily_usd and
// budget.money_monthly_usd). Zero means no cap.
type Caps struct {
	Daily, Monthly float64
}

// CapsOf reads a project's money caps.
func CapsOf(p *project.Project) Caps {
	var c Caps
	_ = project.Field(p, []string{"budget", "money_daily_usd"}, &c.Daily)
	_ = project.Field(p, []string{"budget", "money_monthly_usd"}, &c.Monthly)
	return c
}

// OverMoney reports whether a project has spent past a money cap, and says so.
// Days and months are local time.
func OverMoney(home string, p *project.Project, now time.Time) (bool, string) {
	c := CapsOf(p)
	if c.Daily <= 0 && c.Monthly <= 0 {
		return false, ""
	}
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if c.Daily > 0 {
		if s := Spent(home, p, day); s >= c.Daily {
			return true, fmt.Sprintf("%s spent $%.2f today, at its $%.2f daily cap", p.Name, s, c.Daily)
		}
	}
	if c.Monthly > 0 {
		month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		if s := Spent(home, p, month); s >= c.Monthly {
			return true, fmt.Sprintf("%s spent $%.2f this month, at its $%.2f monthly cap", p.Name, s, c.Monthly)
		}
	}
	return false, ""
}

// SpentBy is what the chats a person started cost since a time, in every project.
func SpentBy(home, person string, since time.Time) float64 {
	total := 0.0
	for _, id := range project.OwnedSessions(home, person) {
		if s, err := LoadSeen(home, id); err == nil && s != nil && !s.At.Before(since) {
			total += s.Cost
		}
	}
	return total
}

// OverPerson reports whether a person reached their own daily or monthly cap.
func OverPerson(home, person string, daily, monthly float64, now time.Time) (bool, string) {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if daily > 0 {
		if s := SpentBy(home, person, day); s >= daily {
			return true, fmt.Sprintf("%s spent $%.2f today, at their $%.2f daily cap", person, s, daily)
		}
	}
	if monthly > 0 {
		month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		if s := SpentBy(home, person, month); s >= monthly {
			return true, fmt.Sprintf("%s spent $%.2f this month, at their $%.2f monthly cap", person, s, monthly)
		}
	}
	return false, ""
}
