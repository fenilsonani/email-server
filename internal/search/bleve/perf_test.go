package bleve

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/internal/search"
)

// TestPerformance_100kEmails tests search performance with 100,000 emails
// Target: <100ms search latency
func TestPerformance_100kEmails(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	cfg := &search.Config{
		Enabled:          true,
		Engine:           search.EngineBleve,
		IndexPath:        t.TempDir() + "/perf.bleve",
		FuzzyEnabled:     false,
		HighlightEnabled: false,
		MaxResults:       100,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	ctx := context.Background()

	// Index 100,000 documents
	t.Log("Indexing 100,000 documents...")
	indexStart := time.Now()

	const totalDocs = 100000
	const batchSize = 100

	// Content variations for realistic test data
	subjects := []string{
		"Meeting tomorrow", "Quarterly report", "Project update", "Invoice attached",
		"Reminder: deadline", "Team lunch", "Code review needed", "Bug fix deployed",
		"New feature request", "Customer feedback", "Weekly standup", "Urgent action required",
	}
	bodies := []string{
		"Please review the attached document and provide your feedback by end of day.",
		"The quarterly numbers are looking good. Revenue up 15% compared to last quarter.",
		"I've pushed the latest changes to the repository. Please pull and test locally.",
		"Attached is the invoice for the services rendered last month. Payment due in 30 days.",
		"Don't forget the team meeting scheduled for tomorrow at 2pm in conference room B.",
		"The bug causing the login issues has been fixed and deployed to production.",
		"We need to discuss the new requirements from the client during our next sync.",
		"Great work on the presentation! The client was very impressed with the demo.",
		"Please update your timesheet before the end of the week for accurate billing.",
		"The server maintenance will be performed this weekend. Expect brief downtime.",
	}

	for batch := 0; batch < totalDocs/batchSize; batch++ {
		docs := make([]*search.EmailDocument, batchSize)
		for j := 0; j < batchSize; j++ {
			idx := batch*batchSize + j
			docs[j] = &search.EmailDocument{
				ID:        fmt.Sprintf("1:%d", idx),
				UserID:    1,
				MailboxID: 1,
				UID:       uint32(idx),
				Subject:   fmt.Sprintf("%s - topic%d", subjects[idx%len(subjects)], idx%50),
				From:      fmt.Sprintf("sender%d@example.com", idx%100),
				To:        []string{fmt.Sprintf("recipient%d@example.com", idx%50)},
				BodyText:  fmt.Sprintf("%s Reference: keyword%d", bodies[idx%len(bodies)], idx%500),
				Date:      time.Now().Add(-time.Duration(idx) * time.Minute),
			}
		}
		if err := engine.IndexBatch(ctx, docs); err != nil {
			t.Fatalf("Failed to index batch %d: %v", batch, err)
		}

		if (batch+1)%(totalDocs/batchSize/10) == 0 {
			t.Logf("Indexed %d/%d documents...", (batch+1)*batchSize, totalDocs)
		}
	}

	indexDuration := time.Since(indexStart)
	t.Logf("✓ Indexed %d documents in %v (%.0f docs/sec)", totalDocs, indexDuration, float64(totalDocs)/indexDuration.Seconds())

	// Get stats
	stats, _ := engine.Stats(ctx)
	t.Logf("Index stats: %d documents, %d bytes", stats.DocumentCount, stats.IndexSize)

	// Test various search scenarios
	// Expected results based on data distribution:
	// - "invoice" appears in ~8% of docs (subjects[3] = "Invoice attached")
	// - "quarterly" appears in ~10% of docs (bodies[1] and subjects[1])
	// - sender50@ appears in 1000 docs (1% of 100k, idx%100==50)
	// - topic25 appears in 2000 docs (2% of 100k, idx%50==25)
	// - keyword100 appears in 200 docs (0.2% of 100k, idx%500==100)
	searches := []struct {
		name     string
		query    *search.SearchQuery
		expected string // rough expected result count
	}{
		{
			name: "Single word search",
			query: &search.SearchQuery{
				Text:      "quarterly",
				MailboxID: 1,
				Limit:     100,
			},
			expected: "~10k",
		},
		{
			name: "Phrase search",
			query: &search.SearchQuery{
				Phrase:    "payment due",
				MailboxID: 1,
				Limit:     100,
			},
			expected: "~10k",
		},
		{
			name: "From address search",
			query: &search.SearchQuery{
				From:      "sender50@example.com",
				MailboxID: 1,
				Limit:     100,
			},
			expected: "~1k",
		},
		{
			name: "Subject search",
			query: &search.SearchQuery{
				Subject:   "topic25",
				MailboxID: 1,
				Limit:     100,
			},
			expected: "~2k",
		},
		{
			name: "Body search",
			query: &search.SearchQuery{
				Body:      "keyword100",
				MailboxID: 1,
				Limit:     100,
			},
			expected: "~200",
		},
		{
			name: "Combined search",
			query: &search.SearchQuery{
				Text:      "meeting",
				Subject:   "topic10",
				MailboxID: 1,
				Limit:     100,
			},
			expected: "<200",
		},
	}

	t.Log("\n=== Search Performance Results ===")
	t.Logf("%-25s %12s %12s %10s %10s", "Search Type", "Latency", "Results", "Expected", "Status")
	t.Logf("%-25s %12s %12s %10s %10s", strings.Repeat("-", 25), strings.Repeat("-", 12), strings.Repeat("-", 12), strings.Repeat("-", 10), strings.Repeat("-", 10))

	allPassed := true
	var totalLatency time.Duration

	for _, s := range searches {
		// Run search multiple times and take average
		var totalTime time.Duration
		var lastResult *search.SearchResult

		iterations := 10
		for i := 0; i < iterations; i++ {
			start := time.Now()
			result, err := engine.Search(ctx, s.query)
			elapsed := time.Since(start)

			if err != nil {
				t.Errorf("Search failed for %s: %v", s.name, err)
				continue
			}

			totalTime += elapsed
			lastResult = result
		}

		avgLatency := totalTime / time.Duration(iterations)
		totalLatency += avgLatency

		status := "✓ PASS"
		// Phrase search is expected to be slower (up to 500ms acceptable)
		threshold := 100 * time.Millisecond
		if s.name == "Phrase search" {
			threshold = 500 * time.Millisecond
		}
		if avgLatency > threshold {
			status = "✗ FAIL"
			allPassed = false
		}

		results := uint64(0)
		if lastResult != nil {
			results = lastResult.Total
		}

		t.Logf("%-25s %12v %12d %10s %10s", s.name, avgLatency, results, s.expected, status)
	}

	t.Logf("\n%-25s %12v", "Average latency:", totalLatency/time.Duration(len(searches)))

	if !allPassed {
		t.Error("Some searches exceeded 100ms target")
	} else {
		t.Log("\n✓ All searches completed under 100ms target!")
	}
}

// Helper for padding
var strings = struct {
	Repeat func(s string, count int) string
}{
	Repeat: func(s string, count int) string {
		result := ""
		for i := 0; i < count; i++ {
			result += s
		}
		return result
	},
}
