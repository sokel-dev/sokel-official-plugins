// catalog：把 TuShare Pro 文档站的接口规格抓成一份 JSON，作为 codegen 的输入。
//
// 为什么自己抓（而不是用 reportify/core/tools/tushare/scraper 那份现成的）：
// 那份是**按表头猜表格类型**的——见「必选」当入参、见「默认显示」当出参。
// 遇到出参表少一列或页面多一张示例表就分错，实测 232 个接口里 29 个字段表是错的，
// 其中 25 个（daily / daily_basic / margin / top10_holders / forecast …）
// 整张出参表被当成入参吞掉，出参为空。生成出来的操作在画布上没有任何输出字段，且不报错。
//
// 这里改成**按段落标记切**：页面结构是
//
//	<p>输入参数</p><table>…</table>
//	<p>输出参数</p><table>…</table>
//
// 标记明确、无歧义；这两个标记之外的表（示例数据）一律不看。
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
					// 分类页与已下线的空页都走这里，不是错误，但要能数得出来。
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

// ===== 数据形状 =====

// API 一个接口页的规格。
type API struct {
	APIName  string  `json:"api_name"` // daily
	Title    string  `json:"title"`    // A股日线行情
	DocID    int     `json:"doc_id"`
	Category string  `json:"category"` // 股票数据/行情数据/历史日线
	Describe string  `json:"describe,omitempty"`
	Inputs   []Param `json:"inputs,omitempty"`
	Outputs  []Param `json:"outputs,omitempty"`
}

// Param 一个入参或出参。
type Param struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required,omitempty"`
	Desc     string `json:"desc,omitempty"`
	// DefaultShow 出参专用：默认是否返回该列。留着是因为 TuShare 的 fields
	// 不传就只回默认列——生成器要用它决定请求里带哪些字段。
	DefaultShow bool `json:"default_show,omitempty"`
}

// node 导航树上的一个页面。
type node struct {
	DocID int
	Name  string
	Path  string
}
