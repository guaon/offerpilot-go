package main

// 牛客网 Go 面试题爬虫
// 用法: go run ./src/cmd/scrape_go
//
// 实现原理:
//  1. practiceHistory 获取当前练习试卷的 paperId
//  2. detail-paper 获取试卷中的题目（含选项和正确答案）
//  3. finish 完成试卷
//  4. makePaper 生成新试卷
//  5. 循环直到所有题目抓完
//
// 要求: 需要有效的 Cookie（从浏览器复制）
// 限制: 如果账号已刷完所有 Go 题，makePaper 会返回"全部完成"
//       需要在浏览器中重置刷题设置，或使用新账号

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ============================================================
// 配置: 替换为你的 Cookie
// 获取: 浏览器 F12 → Application → Cookies → nowcoder.com
// ============================================================
const cookie = ""

var client = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
}

func doPost(url, body string) []byte {
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Referer", "https://www.nowcoder.com/exam/intelligent?questionJobId=10&tagId=809")
	req.Header.Set("Origin", "https://www.nowcoder.com")
	req.Header.Set("x-requested-with", "XMLHttpRequest")

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data
}

func doGet(url string) []byte {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Referer", "https://www.nowcoder.com/exam/intelligent?questionJobId=10&tagId=809")

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data
}

func stripHTML(html string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	return strings.TrimSpace(re.ReplaceAllString(html, ""))
}

func main() {
	if cookie == "" {
		fmt.Println("请先设置 Cookie（编辑 main.go 中的 cookie 常量）")
		os.Exit(1)
	}

	outputDir := "knowledge/go-questions"
	os.MkdirAll(outputDir, 0755)

	seen := map[string]bool{}
	var allQuestions []struct {
		Title   string
		Content string
		Options []string
		Answer  string
	}
	maxRounds := 30

	for round := 0; round < maxRounds; round++ {
		// 1. 获取当前 paperId
		resp := doGet("https://www.nowcoder.com/api/questiontraining/intelligent/practiceHistory?questionJobId=10&tagId=809")
		var phResp struct {
			Data []struct {
				PaperId int `json:"paperId"`
			} `json:"data"`
		}
		json.Unmarshal(resp, &phResp)

		if len(phResp.Data) == 0 || phResp.Data[0].PaperId == 0 {
			// 没有进行中的试卷，生成新试卷
			resp2 := doPost("https://www.nowcoder.com/api/questiontraining/intelligent/makePaper",
				`{"questionJobId":10,"tagIds":[809],"pageSize":10,"source":1,"difficulty":0}`)
			var mpResp struct {
				Code int    `json:"code"`
				Msg  string `json:"msg"`
			}
			json.Unmarshal(resp2, &mpResp)
			if mpResp.Code != 0 {
				fmt.Printf("makePaper: %s (code=%d)\n", mpResp.Msg, mpResp.Code)
				break
			}
			time.Sleep(500 * time.Millisecond)
			continue
		}

		paperId := phResp.Data[0].PaperId

		// 2. 获取题目
		resp3 := doPost("https://gw-c.nowcoder.com/api/sparta/test/detail-paper",
			fmt.Sprintf(`{"paperId":%d}`, paperId))
		var detail struct {
			Data struct {
				PaperQuestionDetails []struct {
					Id      int    `json:"id"`
					Title   string `json:"title"`
					Content string `json:"content"`
					ChooseAnswer []struct {
						Content string `json:"content"`
						Choosed bool   `json:"choosed"`
					} `json:"chooseAnswer"`
				} `json:"paperQuestionDetails"`
			} `json:"data"`
		}
		json.Unmarshal(resp3, &detail)

		roundNew := 0
		for _, q := range detail.Data.PaperQuestionDetails {
			title := q.Title
			if title == "" {
				title = stripHTML(q.Content)
			}
			if title == "" || seen[title] {
				continue
			}
			seen[title] = true
			roundNew++

			var options []string
			var correct string
			for _, opt := range q.ChooseAnswer {
				t := stripHTML(opt.Content)
				options = append(options, t)
				if opt.Choosed {
					correct = t
				}
			}
			allQuestions = append(allQuestions, struct {
				Title   string
				Content string
				Options []string
				Answer  string
			}{title, stripHTML(q.Content), options, correct})
		}

		fmt.Printf("第%d轮: %d 新题 (累计 %d)\n", round+1, roundNew, len(allQuestions))

		// 3. 完成试卷
		doPost("https://gw-c.nowcoder.com/api/sparta/test/finish",
			fmt.Sprintf(`{"paperId":%d}`, paperId))
		time.Sleep(500 * time.Millisecond)

		if roundNew == 0 {
			break
		}
	}

	fmt.Printf("\n总计: %d 道 Go 题\n\n", len(allQuestions))

	// 生成 KB 格式 .md 文件
	groupSize := 5
	for i := 0; i < len(allQuestions); i += groupSize {
		end := i + groupSize
		if end > len(allQuestions) {
			end = len(allQuestions)
		}
		group := allQuestions[i:end]
		fileName := fmt.Sprintf("go_questions_%03d.md", i/groupSize+1)
		filePath := filepath.Join(outputDir, fileName)

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("# Go 面试题 (第%d组)\n\n", i/groupSize+1))
		sb.WriteString("来源: 牛客网智能刷题 - Go专项\n\n---\n\n")

		for j, q := range group {
			sb.WriteString(fmt.Sprintf("## Q%d：%s\n\n", j+1, q.Title))
			if q.Content != "" && q.Content != q.Title {
				sb.WriteString(fmt.Sprintf("%s\n\n", q.Content))
			}
			sb.WriteString("**新手答**：见下方选项。\n\n")
			sb.WriteString("**高手答**：\n")
			for _, opt := range q.Options {
				marker := "  "
				if opt == q.Answer {
					marker = "✅"
				}
				sb.WriteString(fmt.Sprintf("- %s %s\n", marker, opt))
			}
			sb.WriteString("\n**差距在哪**：新手可能只凭直觉选，高手能准确理解 Go 语言的设计原理和最佳实践。\n\n")
			sb.WriteString("---\n\n")
		}
		os.WriteFile(filePath, []byte(sb.String()), 0644)
		fmt.Printf("  写入: %s\n", fileName)
	}
}