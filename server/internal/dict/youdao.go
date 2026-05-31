package dict

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const youdaoSuggestAPI = "https://dict.youdao.com/suggest"

type YoudaoResponse struct {
	Result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	} `json:"result"`
	Data struct {
		Entries []struct {
			Explain string `json:"explain"`
			Entry   string `json:"entry"`
		} `json:"entries"`
	} `json:"data"`
}

// FetchFromYoudao 从有道查词
func FetchFromYoudao(word string) (*YoudaoResponse, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("%s?num=1&doctype=json&q=%s", youdaoSuggestAPI, word))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youdao returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result YoudaoResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.Result.Code != 200 {
		return nil, fmt.Errorf("youdao error: %s", result.Result.Msg)
	}
	return &result, nil
}

// ToWordResult 转换为统一格式
func (r *YoudaoResponse) ToWordResult() *wordResultRaw {
	if len(r.Data.Entries) == 0 {
		return nil
	}
	entry := r.Data.Entries[0]
	res := &wordResultRaw{
		Word:    entry.Entry,
		Sources: []sourceEntry{{Platform: "youdao", Model: ""}},
	}
	// 有道 suggest API 返回的是简要释义字符串，按"; "分割
	// 由于没有词性分类，统一作为其他
	res.Meanings = []meaningEntry{{
		PartOfSpeech: "",
		Definitions:  splitExplain(entry.Explain),
	}}
	return res
}

func splitExplain(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ';' && i+1 < len(s) && s[i+1] == ' ' {
			parts = append(parts, s[start:i])
			start = i + 2
		}
	}
	if start < len(s) {
		parts = append(parts, s[start:])
	}
	if len(parts) == 0 {
		parts = append(parts, s)
	}
	return parts
}
