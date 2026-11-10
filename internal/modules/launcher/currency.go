package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ratesEndpoint serves the European Central Bank's reference rates against
// the euro, updated on working days. It needs no key.
const ratesEndpoint = "https://api.frankfurter.dev/v1/latest"

// ratesFresh is how long fetched rates are used before fetching again.
const ratesFresh = 6 * time.Hour

// currencies are the codes the reference rates cover.
var currencies = strings.Fields(`AUD BRL CAD CHF CNY CZK DKK EUR GBP HKD HUF IDR ILS INR ISK
	JPY KRW MXN MYR NOK NZD PHP PLN RON SEK SGD THB TRY USD ZAR`)

// currencyNames maps the ways people write currencies to their ISO codes.
var currencyNames = map[string]string{
	"$": "USD", "美元": "USD", "美金": "USD", "刀": "USD",
	"¥": "CNY", "￥": "CNY", "元": "CNY", "块": "CNY", "人民币": "CNY", "rmb": "CNY",
	"€": "EUR", "欧元": "EUR",
	"£": "GBP", "英镑": "GBP",
	"円": "JPY", "日元": "JPY", "日币": "JPY",
	"₩": "KRW", "韩元": "KRW", "韩币": "KRW",
	"港币": "HKD", "港元": "HKD", "港纸": "HKD",
	"澳元": "AUD", "澳币": "AUD",
	"加元": "CAD", "加币": "CAD",
	"新币": "SGD", "新加坡元": "SGD",
	"瑞郎": "CHF", "瑞士法郎": "CHF",
	"泰铢":  "THB",
	"卢比":  "INR",
	"林吉特": "MYR",
}

// currencyPattern reads "100 usd", "$100", "100美元", "100 usd to jpy" and
// "100 美元换日元".
var currencyPattern = regexp.MustCompile(
	`^\s*([^\d\s.,]{0,6}?)\s*([\d][\d,]*(?:\.\d+)?)\s*([^\d\s]{0,6}?)\s*(?:(?:to|in|=|->|换成|换|兑|转)\s*([^\d\s]{1,6}))?\s*$`)

// currencyCode turns a currency as written into the ISO code of one the
// rates cover, or "".
func currencyCode(s string) string {
	s = strings.TrimSpace(s)
	if code, ok := currencyNames[strings.ToLower(s)]; ok {
		return code
	}
	if code := strings.ToUpper(s); slices.Contains(currencies, code) {
		return code
	}
	return ""
}

// conversion is an amount to convert, read from a query.
type conversion struct {
	amount   float64
	from, to string // to is "" when the query names no target
}

// parseConversion reads a currency conversion from a query.
func parseConversion(q string) (conversion, bool) {
	m := currencyPattern.FindStringSubmatch(strings.ToLower(q))
	if m == nil {
		return conversion{}, false
	}
	before, number, after, target := m[1], m[2], m[3], m[4]
	if (before == "") == (after == "") {
		return conversion{}, false
	}
	from := currencyCode(before + after)
	amount, err := strconv.ParseFloat(strings.ReplaceAll(number, ",", ""), 64)
	if from == "" || err != nil {
		return conversion{}, false
	}
	c := conversion{amount: amount, from: from}
	if target != "" {
		if c.to = currencyCode(target); c.to == "" {
			return conversion{}, false
		}
	}
	return c, true
}

// rates fetches and caches the reference rates.
type rates struct {
	client *http.Client

	mu      sync.Mutex
	perEuro map[string]float64
	date    string
	fetched time.Time
}

// get returns the rates against the euro and the day they are for.
func (r *rates) get(ctx context.Context) (map[string]float64, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.perEuro != nil && time.Since(r.fetched) < ratesFresh {
		return r.perEuro, r.date, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ratesEndpoint, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return r.stale(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		return r.stale(errors.New("汇率服务不可用"))
	}
	var data struct {
		Date  string             `json:"date"`
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.Unmarshal(body, &data); err != nil || len(data.Rates) == 0 {
		return r.stale(errors.New("汇率服务返回的内容无法识别"))
	}
	data.Rates["EUR"] = 1
	r.perEuro, r.date, r.fetched = data.Rates, data.Date, time.Now()
	return r.perEuro, r.date, nil
}

// stale falls back to older rates when fetching fails. r.mu must be held.
func (r *rates) stale(err error) (map[string]float64, string, error) {
	if r.perEuro != nil {
		return r.perEuro, r.date, nil
	}
	return nil, "", err
}

// convert answers a conversion: into its target, or into the home currency
// and US dollars when it names none.
func convert(c conversion, perEuro map[string]float64, date, home string) []Result {
	fromRate, ok := perEuro[c.from]
	if !ok {
		return nil
	}
	targets := []string{c.to}
	if c.to == "" {
		targets = []string{home, "USD", "EUR"}
	}
	var results []Result
	for _, to := range targets {
		toRate, ok := perEuro[to]
		if !ok || to == c.from || len(results) == 2 {
			continue
		}
		value := c.amount / fromRate * toRate
		amount := strconv.FormatFloat(value, 'f', 2, 64)
		results = append(results, Result{
			Kind:     kindCurrency,
			Key:      amount,
			Title:    amount + " " + to,
			Subtitle: formatNumber(c.amount) + " " + c.from + " · 欧洲央行 " + date + " 汇率",
		})
	}
	return results
}
