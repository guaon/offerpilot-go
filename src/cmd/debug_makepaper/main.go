package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	cookie := os.Getenv("NOWCODER_COOKIE")
	if cookie == "" {
		fmt.Fprintln(os.Stderr, "NOWCODER_COOKIE is required")
		os.Exit(1)
	}

	do := func(method, url, body string) {
		var req *http.Request
		var err error
		if method == "GET" {
			req, err = http.NewRequest("GET", url, nil)
		} else {
			req, err = http.NewRequest("POST", url, strings.NewReader(body))
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "create request: %v\n", err)
			return
		}
		if method != "GET" {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("User-Agent", "Mozilla/5.0")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Cookie", cookie)
		req.Header.Set("Referer", "https://www.nowcoder.com/exam/intelligent?questionJobId=10&tagId=809")
		req.Header.Set("Origin", "https://www.nowcoder.com")
		req.Header.Set("x-requested-with", "XMLHttpRequest")

		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s %s: %v\n", method, url, err)
			return
		}
		defer resp.Body.Close()
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read response: %v\n", err)
			return
		}
		preview := string(respBody)
		if len(preview) > 2000 {
			preview = preview[:2000]
		}
		fmt.Printf("%s %s\n  -> %d\n  %s\n\n", method, url, resp.StatusCode, preview)
	}

	// 1. 查询 Go 的所有子标签
	fmt.Println("=== newjob/getTags ===")
	do("GET", "https://www.nowcoder.com/api/questiontraining/intelligent/newjob/getTags?questionJobId=10", "")

	// 2. makePaper 用 knowledgeIds 和 source=0
	fmt.Println("=== makePaper source=0, knowledgeIds=[809] ===")
	do("POST", "https://www.nowcoder.com/api/questiontraining/intelligent/makePaper",
		`{"questionJobId":10,"knowledgeIds":[809],"pageSize":10,"source":0}`)

	// 3. makePaper source=1, knowledgeIds
	fmt.Println("=== makePaper source=1, knowledgeIds=[809] ===")
	do("POST", "https://www.nowcoder.com/api/questiontraining/intelligent/makePaper",
		`{"questionJobId":10,"knowledgeIds":[809],"pageSize":10,"source":1}`)

	// 4. 尝试 questionJobId=10 下的所有 source 值
	fmt.Println("=== makePaper questionJobId=10, source=0,1,2,3 ===")
	for _, s := range []int{0, 1, 2, 3} {
		do("POST", "https://www.nowcoder.com/api/questiontraining/intelligent/makePaper",
			fmt.Sprintf(`{"questionJobId":10,"pageSize":10,"source":%d}`, s))
		time.Sleep(200 * time.Millisecond)
	}
}
