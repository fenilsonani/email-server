package admin

import (
	"fmt"
	"net/http"
	"time"
)

// --- Analytics ---

// handleAPIAnalytics returns email analytics data
func (s *Server) handleAPIAnalytics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx := r.Context()
	rangeParam := r.URL.Query().Get("range")
	if rangeParam != "30d" {
		rangeParam = "7d"
	}

	var since time.Time
	var groupFormat, groupLabel string
	if rangeParam == "30d" {
		since = time.Now().AddDate(0, 0, -30)
		groupFormat = "%Y-%m-%d"
		groupLabel = "day"
	} else {
		since = time.Now().AddDate(0, 0, -7)
		groupFormat = "%Y-%m-%dT%H:00:00Z"
		groupLabel = "hour"
	}
	_ = groupLabel

	sinceStr := since.UTC().Format("2006-01-02 15:04:05")

	// Time series: inbound vs outbound over time
	type TimePoint struct {
		Time     string `json:"time"`
		Inbound  int    `json:"inbound"`
		Outbound int    `json:"outbound"`
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT strftime('%s', created_at) as t, direction, COUNT(*) as cnt
		FROM delivery_log
		WHERE created_at >= ?
		GROUP BY t, direction
		ORDER BY t
	`, groupFormat), sinceStr)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to query analytics")
		return
	}
	defer rows.Close()

	timeMap := make(map[string]*TimePoint)
	var timeOrder []string
	for rows.Next() {
		var t, direction string
		var count int
		if err := rows.Scan(&t, &direction, &count); err != nil {
			continue
		}
		tp, ok := timeMap[t]
		if !ok {
			tp = &TimePoint{Time: t}
			timeMap[t] = tp
			timeOrder = append(timeOrder, t)
		}
		if direction == "inbound" {
			tp.Inbound = count
		} else {
			tp.Outbound = count
		}
	}

	timeSeries := make([]TimePoint, 0, len(timeOrder))
	for _, t := range timeOrder {
		timeSeries = append(timeSeries, *timeMap[t])
	}

	// Summary stats
	type Summary struct {
		TotalInbound  int `json:"total_inbound"`
		TotalOutbound int `json:"total_outbound"`
		Bounces       int `json:"bounces"`
		AvgSizeBytes  int `json:"avg_size_bytes"`
	}
	var summary Summary

	s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(CASE WHEN direction='inbound' THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN direction='outbound' THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN status='bounced' THEN 1 ELSE 0 END), 0)
		 FROM delivery_log WHERE created_at >= ?`, sinceStr,
	).Scan(&summary.TotalInbound, &summary.TotalOutbound, &summary.Bounces)

	// Top domains (from recipient addresses)
	type DomainCount struct {
		Domain string `json:"domain"`
		Count  int    `json:"count"`
	}

	domainRows, err := s.db.QueryContext(ctx, `
		SELECT CASE
			WHEN INSTR(recipient, '@') > 0 THEN SUBSTR(recipient, INSTR(recipient, '@') + 1)
			ELSE recipient
		END as domain, COUNT(*) as cnt
		FROM delivery_log
		WHERE created_at >= ?
		GROUP BY domain
		ORDER BY cnt DESC
		LIMIT 10
	`, sinceStr)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to query top domains")
		return
	}
	defer domainRows.Close()

	topDomains := []DomainCount{}
	for domainRows.Next() {
		var d DomainCount
		if err := domainRows.Scan(&d.Domain, &d.Count); err == nil {
			topDomains = append(topDomains, d)
		}
	}

	// Top senders
	type SenderCount struct {
		Email string `json:"email"`
		Count int    `json:"count"`
	}

	senderRows, err := s.db.QueryContext(ctx, `
		SELECT sender, COUNT(*) as cnt
		FROM delivery_log
		WHERE created_at >= ? AND sender != ''
		GROUP BY sender
		ORDER BY cnt DESC
		LIMIT 10
	`, sinceStr)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to query top senders")
		return
	}
	defer senderRows.Close()

	topSenders := []SenderCount{}
	for senderRows.Next() {
		var sc SenderCount
		if err := senderRows.Scan(&sc.Email, &sc.Count); err == nil {
			topSenders = append(topSenders, sc)
		}
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"time_series":  timeSeries,
		"summary":      summary,
		"top_domains":  topDomains,
		"top_senders":  topSenders,
		"range":        rangeParam,
	})
}
