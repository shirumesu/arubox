package launcher

import (
	"fmt"
	"strings"
	"time"

	// Windows has no time zone database of its own.
	_ "time/tzdata"
)

// cities maps city names, in Chinese and English, to their time zones.
var cities = []struct {
	names []string
	zone  string
}{
	{[]string{"北京", "beijing", "上海", "shanghai", "中国", "china"}, "Asia/Shanghai"},
	{[]string{"香港", "hong kong", "hongkong"}, "Asia/Hong_Kong"},
	{[]string{"台北", "taipei", "台湾", "taiwan"}, "Asia/Taipei"},
	{[]string{"东京", "tokyo", "大阪", "osaka", "日本", "japan"}, "Asia/Tokyo"},
	{[]string{"首尔", "seoul", "韩国", "korea"}, "Asia/Seoul"},
	{[]string{"新加坡", "singapore"}, "Asia/Singapore"},
	{[]string{"曼谷", "bangkok", "泰国", "thailand"}, "Asia/Bangkok"},
	{[]string{"吉隆坡", "kuala lumpur", "马来西亚", "malaysia"}, "Asia/Kuala_Lumpur"},
	{[]string{"雅加达", "jakarta", "印尼", "indonesia"}, "Asia/Jakarta"},
	{[]string{"河内", "hanoi", "胡志明市", "ho chi minh", "越南", "vietnam"}, "Asia/Ho_Chi_Minh"},
	{[]string{"马尼拉", "manila", "菲律宾", "philippines"}, "Asia/Manila"},
	{[]string{"新德里", "new delhi", "delhi", "孟买", "mumbai", "印度", "india"}, "Asia/Kolkata"},
	{[]string{"迪拜", "dubai", "阿联酋", "uae"}, "Asia/Dubai"},
	{[]string{"莫斯科", "moscow", "俄罗斯", "russia"}, "Europe/Moscow"},
	{[]string{"伊斯坦布尔", "istanbul", "土耳其", "turkey"}, "Europe/Istanbul"},
	{[]string{"伦敦", "london", "英国", "uk"}, "Europe/London"},
	{[]string{"巴黎", "paris", "法国", "france"}, "Europe/Paris"},
	{[]string{"柏林", "berlin", "德国", "germany"}, "Europe/Berlin"},
	{[]string{"罗马", "rome", "意大利", "italy"}, "Europe/Rome"},
	{[]string{"马德里", "madrid", "西班牙", "spain"}, "Europe/Madrid"},
	{[]string{"阿姆斯特丹", "amsterdam", "荷兰", "netherlands"}, "Europe/Amsterdam"},
	{[]string{"苏黎世", "zurich", "瑞士", "switzerland"}, "Europe/Zurich"},
	{[]string{"开罗", "cairo", "埃及", "egypt"}, "Africa/Cairo"},
	{[]string{"约翰内斯堡", "johannesburg", "南非", "south africa"}, "Africa/Johannesburg"},
	{[]string{"纽约", "new york", "nyc", "华盛顿", "washington", "美东"}, "America/New_York"},
	{[]string{"芝加哥", "chicago"}, "America/Chicago"},
	{[]string{"丹佛", "denver"}, "America/Denver"},
	{[]string{"洛杉矶", "los angeles", "la", "旧金山", "san francisco", "西雅图", "seattle", "硅谷", "美西"}, "America/Los_Angeles"},
	{[]string{"多伦多", "toronto"}, "America/Toronto"},
	{[]string{"温哥华", "vancouver"}, "America/Vancouver"},
	{[]string{"墨西哥城", "mexico city"}, "America/Mexico_City"},
	{[]string{"圣保罗", "sao paulo", "巴西", "brazil"}, "America/Sao_Paulo"},
	{[]string{"悉尼", "sydney", "墨尔本", "melbourne"}, "Australia/Sydney"},
	{[]string{"奥克兰", "auckland", "新西兰", "new zealand"}, "Pacific/Auckland"},
	{[]string{"utc", "gmt"}, "UTC"},
}

// timeWords mark a query as asking for the time.
var timeWords = []string{"现在几点", "几点了", "几点", "时间", "time"}

// worldTime answers "东京时间", "time in tokyo", "london time" or just a city
// name with the time there.
func worldTime(query string, now time.Time) (Result, bool) {
	q := strings.ToLower(strings.TrimSpace(query))
	asked := false
	for _, w := range timeWords {
		if rest, ok := strings.CutSuffix(q, w); ok {
			q, asked = rest, true
			break
		}
		if rest, ok := strings.CutPrefix(q, w); ok {
			q, asked = rest, true
			break
		}
	}
	q = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(q), "in "))
	q = strings.TrimSuffix(q, "的")
	if q == "" {
		return Result{}, false
	}
	for _, c := range cities {
		for _, name := range c.names {
			// Without a word asking for the time, short Latin names such as
			// "la" are too likely to be the start of something else.
			if name != q || (!asked && len(name) < 5 && isASCII(name)) {
				continue
			}
			zone, err := time.LoadLocation(c.zone)
			if err != nil {
				return Result{}, false
			}
			there := now.In(zone)
			return Result{
				Kind:     kindTime,
				Key:      there.Format("15:04"),
				Title:    there.Format("15:04") + " · " + dateText(there),
				Subtitle: c.names[0] + " · " + offsetText(there, now),
			}, true
		}
	}
	return Result{}, false
}

var weekdays = []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}

func dateText(t time.Time) string {
	return fmt.Sprintf("%d月%d日 %s", t.Month(), t.Day(), weekdays[t.Weekday()])
}

// offsetText names the zone's UTC offset and how far it is from here.
func offsetText(there, here time.Time) string {
	_, theirs := there.Zone()
	_, ours := here.Zone()
	utc := "UTC" + hoursText(theirs, true)
	switch diff := theirs - ours; {
	case diff == 0:
		return utc + " · 与本地相同"
	case diff > 0:
		return utc + " · 比本地快 " + hoursText(diff, false) + " 小时"
	default:
		return utc + " · 比本地慢 " + hoursText(-diff, false) + " 小时"
	}
}

// hoursText prints a number of seconds as hours, with half hours as .5.
func hoursText(seconds int, signed bool) string {
	h := float64(seconds) / 3600
	s := formatNumber(h)
	if signed && h >= 0 {
		s = "+" + s
	}
	return s
}

func isASCII(s string) bool {
	for _, r := range s {
		if r >= 0x80 {
			return false
		}
	}
	return true
}
