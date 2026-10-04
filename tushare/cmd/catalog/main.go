// catalog scrapes the TuShare Pro doc site's endpoint specs into a single JSON file, as the input
// for codegen.
//
// Why scrape it ourselves (instead of using the one already in reportify/core/tools/tushare/scraper):
// that one **guesses the table type from the column headers** — seeing "必选" (required) it treats
// the table as inputs, seeing "默认显示" (default shown) it treats it as outputs. It misclassifies
// whenever an outputs table is missing a column or a page has an extra sample table; in practice 29
// of the field tables across 232 endpoints were wrong, and in 25 of those (daily / daily_basic /
// margin / top10_holders / forecast, …) the entire outputs table got swallowed as inputs, leaving
// outputs empty. The generated operation then has zero output fields on the canvas, with no error.
//
// This one instead **splits by paragraph markers**: the page structure is
//
//	<p>输入参数</p><table>…</table>  (input parameters)
//	<p>输出参数</p><table>…</table>  (output parameters)
//
// The markers are explicit and unambiguous; any table outside these two markers (sample data) is
// simply ignored.
//
//	go run ./cmd/catalog -out catalog/tushare-apis.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

func main() {
	out := flag.String("out", "catalog/tushare-apis.json", "输出文件")
	workers := flag.Int("workers", 3, "并发数（对人家的站客气点）")
	limit := flag.Int("limit", 0, "只抓前 N 个（调试用，0=全部）")
	flag.Parse()

	if err := run(*out, *workers, *limit); err != nil {
		fmt.Fprintln(os.Stderr, "catalog:", err)
		os.Exit(1)
	}
}

func run(outPath string, workers, limit int) error {
	nodes, err := fetchIndex()
	if err != nil {
		return err
	}
	fmt.Printf("文档导航：%d 个页面\n", len(nodes))
	if limit > 0 && limit < len(nodes) {
		nodes = nodes[:limit]
	}

	apis := make([]API, len(nodes))
	var (
		wg      sync.WaitGroup
		jobs    = make(chan int)
		mu      sync.Mutex
		skipped []string
		done    int
	)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				api, why, err := fetchDetail(nodes[i])
				mu.Lock()
				done++
				switch {
				case err != nil:
					skipped = append(skipped, fmt.Sprintf("doc_id=%d %s: %v", nodes[i].DocID, nodes[i].Path, err))
				case api == nil:
					// Category pages and decommissioned empty pages both land here; it's not an
					// error, but it still needs to be countable.
					skipped = append(skipped, fmt.Sprintf("doc_id=%d %s: %s", nodes[i].DocID, nodes[i].Path, why))
				default:
					apis[i] = *api
				}
				if done%40 == 0 {
					fmt.Printf("  %d/%d\n", done, len(nodes))
				}
				mu.Unlock()
				time.Sleep(300 * time.Millisecond)
			}
		}()
	}
	for i := range nodes {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	var kept []API
	for _, a := range apis {
		if a.APIName != "" {
			kept = append(kept, a)
		}
	}
	sort.Slice(kept, func(i, j int) bool {
		if kept[i].APIName != kept[j].APIName {
			return kept[i].APIName < kept[j].APIName
		}
		return kept[i].DocID < kept[j].DocID
	})

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(kept, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(outPath, b, 0o644); err != nil {
		return err
	}

	fmt.Printf("已写入 %s：%d 个接口页\n", outPath, len(kept))
	fmt.Printf("跳过 %d 个（分类页 / 已下线 / 无参数表）：\n", len(skipped))
	sort.Strings(skipped)
	for _, s := range skipped {
		fmt.Println("  " + s)
	}
	return nil
}

// ===== Data shapes =====

// API is one endpoint page's spec.
type API struct {
	APIName  string  `json:"api_name"` // e.g. daily
	Title    string  `json:"title"`    // e.g. A-share daily quotes
	DocID    int     `json:"doc_id"`
	Category string  `json:"category"` // e.g. Stock data/Quotes data/Historical daily bars
	Describe string  `json:"describe,omitempty"`
	Inputs   []Param `json:"inputs,omitempty"`
	Outputs  []Param `json:"outputs,omitempty"`
}

// Param is one input or output parameter.
type Param struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required,omitempty"`
	Desc     string `json:"desc,omitempty"`
	// DefaultShow is output-only: whether this column is returned by default. Kept because
	// TuShare's fields only returns the default columns when it isn't passed — the generator uses
	// this to decide which fields to include in the request.
	DefaultShow bool `json:"default_show,omitempty"`
}

// node is one page in the navigation tree.
type node struct {
	DocID int
	Name  string
	Path  string
}
