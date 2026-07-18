package knowledge

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

type parseQuestion struct {
	question     string
	noviceAnswer string
	expertAnswer string
	tags         []string
}

func ParseKnowledgeDir(dirPath string) ([]*KnowledgeEntry, error) {
	var entries []*KnowledgeEntry
	err := filepath.WalkDir(dirPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(dirPath, path) //获取文件相对于根目录的路径
		if err != nil {
			return err
		}

		dimension := detectDimension(relPath)
		questions := extractQuestions(string(content))

		if len(questions) > 0 {
			for _, q := range questions {

				entries = append(entries, &KnowledgeEntry{
					ID:           uuid.New().String(),
					Title:        q.question[:min(len(q.question), 100)],
					Dimension:    dimension,
					Content:      buildContent(q),
					SourceFile:   relPath,
					Question:     q.question,
					ExpertAnswer: q.expertAnswer,
					NoviceAnswer: q.noviceAnswer,
					Tags:         q.tags,
				})
			}
		} else {
			title := extractTitle(string(content))
			if title == "" {
				title = relPath
			}
			entries = append(entries, &KnowledgeEntry{
				ID:         uuid.New().String(),
				Title:      title,
				Dimension:  dimension,
				Content:    string(content),
				SourceFile: relPath,
			})

		}

		return nil
	})

	return entries, err
}

func detectDimension(path string) string {
	dimensionMap := map[string]string{
		"01-architecture":      "architecture",
		"02-engineering":       "engineering",
		"02-tool":              "engineering",
		"03-model":             "model",
		"03-fault":             "engineering",
		"04-rag":               "rag",
		"04-memory":            "rag",
		"05-multi-agent":       "multi-agent",
		"05-eval":              "evaluation",
		"06-evaluation":        "evaluation",
		"06-multi-agent":       "multi-agent",
		"07-full-stack":        "full-stack",
		"07-engineering":       "engineering",
		"08-prompt":            "model",
		"09-rag":               "rag",
		"10-training":          "model",
		"11-ai-code":           "full-stack",
		"12-business":          "full-stack",
		"13-project":           "architecture",
		"14-company":           "architecture",
		"15-agent":             "architecture",
		"coaching-methodology": "coaching",
	}

	for prefix, dim := range dimensionMap {
		if strings.Contains(path, prefix) {
			return dim
		}
	}
	return "general"
}

func extractQuestions(content string) []parseQuestion {
	var questions []parseQuestion
	qBlocks := regexp.MustCompile(`^#{2,3}\s*Q[：:]`).Split(content, -1) //按问答标记分割内容
	if len(qBlocks) <= 1 {
		return questions
	}

	for _, block := range qBlocks[1:] { //跳过第一个空元素
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}

		//提取问题文本
		questionMatch := regexp.MustCompile(`^(.+?)(?:\n|$)`).FindStringSubmatch(block)
		if len(questionMatch) < 2 {
			continue

		}
		question := strings.TrimSpace(questionMatch[1])

		//提取新手答
		noviceMatch := regexp.MustCompile(`\*\*新手答\*\*[：:]?\s*"?(.+?)"?\s*(?:\n|$)`).FindStringSubmatch(block)

		//提取高手答
		expertStartMatch := regexp.MustCompile(`\*\*高手答\*\*[：:]?\s*\n`).FindStringIndex(block)
		var expertAnswer string
		if expertStartMatch != nil {
			startIdx := expertStartMatch[1] //找到高手答的介素位置
			remaining := block[startIdx:]
			endMatch := regexp.MustCompile(`\n(?:\*\*差距在哪|---|\n#{2,3}\s|^\*\*追问)`).FindStringIndex(remaining) //查找借宿标记
			if endMatch != nil {
				expertAnswer = strings.TrimSpace(remaining[:endMatch[0]])
			} else {
				expertAnswer = strings.TrimSpace(remaining)
			}
			if len(expertAnswer) > 2000 {
				expertAnswer = expertAnswer[:2000]
			}

		}

		questions = append(questions, parseQuestion{
			question:     question,
			noviceAnswer: strings.TrimSpace(noviceMatch[1]),
			expertAnswer: expertAnswer,
		})

	}

	return questions

}

func extractTitle(content string) string {
	match := regexp.MustCompile(`^#\s+(.+)$`).FindStringSubmatch(content)
	if len(match) >= 2 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func buildContent(q parseQuestion) string {
	var sb strings.Builder
	sb.WriteString("问题：")
	sb.WriteString(q.question)
	sb.WriteString("\n")

	if q.noviceAnswer != "" {
		sb.WriteString("\n新手答：")
		sb.WriteString(q.noviceAnswer)
		sb.WriteString("\n")
	}

	if q.expertAnswer != "" {
		sb.WriteString("\n高手答：")
		sb.WriteString(q.expertAnswer)
		sb.WriteString("\n")
	}

	return sb.String()

}
