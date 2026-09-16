package main

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

type Result struct {
	URL        string
	StatusCode int
	Duration   time.Duration
	Err        error
}

type Checker interface {
	Check(url string) Result
}

type HTTPChecker struct {
	Client http.Client
}

func (h *HTTPChecker) Check(url string) Result {
	start := time.Now()
	resp, err := h.Client.Get(url)
	duration := time.Since(start)

	if err != nil {
		return Result{
			URL:      url,
			Duration: duration,
			Err:      err,
		}
	}
	defer resp.Body.Close()

	return Result{
		URL:        url,
		StatusCode: resp.StatusCode,
		Duration:   duration,
	}
}

type Stats struct {
	Total   int
	Success int
	Errors  int
}

func main() {
	urls := []string{
		"https://go.dev",
		"https://github.com",
		"https://www.google.com",
		"https://www.wikipedia.org",
		"https://student-grade-web.onrender.com",
		"https://invalid-fake-host-test.org",
		"https://httpbin.org/status/200",
		"https://httpbin.org/status/404",
		"https://httpbin.org/status/500",
		"https://httpbin.org/delay/65",
		"https://invalid-fake-host-test.org",
	}

	checker := &HTTPChecker{
		Client: http.Client{Timeout: 75 * time.Second},
	}

	
	seqStart := time.Now()
	for _, u := range urls {
		_ = checker.Check(u)
	}
	seqDuration := time.Since(seqStart)

	
	fmt.Println("=== HTTP URL Health Checker ===")
	fmt.Println("\n[Конкурентна перевірка адрес...]")

	concStart := time.Now()

	resultsChan := make(chan Result, len(urls))
	var wg sync.WaitGroup

	for _, u := range urls {
		wg.Add(1)

		go func(targetURL string) {
			defer wg.Done()
			res := checker.Check(targetURL)
			resultsChan <- res
		}(u)
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	var stats Stats
	for res := range resultsChan {
		stats.Total++
		if res.Err != nil {
			stats.Errors++
			fmt.Printf("-> %s: Мережева помилка (%v)\n", res.URL, res.Err)
		} else if res.StatusCode >= 200 && res.StatusCode < 300 {
			stats.Success++
			fmt.Printf("-> %s: Status %d %s (час: %v)\n", res.URL, res.StatusCode, http.StatusText(res.StatusCode), res.Duration.Round(time.Millisecond))
		} else {
			stats.Errors++ // Запити зі статусами 4xx та 5xx тепер також враховуються як помилки
			fmt.Printf("-> %s: Помилка HTTP %d %s (час: %v)\n", res.URL, res.StatusCode, http.StatusText(res.StatusCode), res.Duration.Round(time.Millisecond))
		}
	}
	concDuration := time.Since(concStart)

	
	fmt.Println("\n==========================================")
	fmt.Println("ПІДСУМКОВИЙ ЗВІТ МОНІТОРИНГУ:")
	fmt.Printf("- Усього перевірено адрес: %d\n", stats.Total)
	fmt.Printf("- Успішних з'єднань: %d\n", stats.Success)
	fmt.Printf("- Мережевих помилок: %d\n", stats.Errors)
	fmt.Printf("- Загальний час роботи: %v (проти %v у послідовному режимі)\n",
		concDuration.Round(time.Millisecond),
		seqDuration.Round(time.Millisecond),
	)
	fmt.Println("==========================================")
}
