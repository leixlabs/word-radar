package dict

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const dictAPIBase = "https://api.dictionaryapi.dev/api/v2/entries/en"

type DictAPIResponse []struct {
	Word      string `json:"word"`
	Phonetic  string `json:"phonetic"`
	Phonetics []struct {
		Text      string `json:"text"`
		Audio     string `json:"audio"`
		SourceURL string `json:"sourceUrl"`
	} `json:"phonetics"`
	Meanings []struct {
		PartOfSpeech string `json:"partOfSpeech"`
		Definitions  []struct {
			Definition string `json:"definition"`
			Example    string `json:"example"`
		} `json:"definitions"`
	} `json:"meanings"`
	SourceUrls []string `json:"sourceUrls"`
}

// FetchFromDictionaryAPI 从 dictionaryapi.dev 查词
func FetchFromDictionaryAPI(word string) (*DictAPIResponse, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("%s/%s", dictAPIBase, word))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dictionaryapi returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result DictAPIResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ToWordResult 转换为统一格式
func (r *DictAPIResponse) ToWordResult() *wordResultRaw {
	if len(*r) == 0 {
		return nil
	}
	entry := (*r)[0]
	res := &wordResultRaw{
		Word:     entry.Word,
		Phonetic: entry.Phonetic,
		Sources:  []sourceEntry{{Platform: "dictionaryapi", Model: ""}},
	}

	// 取第一个有 audio 的 phonetic
	for _, p := range entry.Phonetics {
		if p.Audio != "" {
			res.AudioURL = p.Audio
			if res.Phonetic == "" && p.Text != "" {
				res.Phonetic = p.Text
			}
			break
		}
	}
	if res.Phonetic == "" && len(entry.Phonetics) > 0 {
		res.Phonetic = entry.Phonetics[0].Text
	}

	for _, m := range entry.Meanings {
		meaning := meaningEntry{
			PartOfSpeech: m.PartOfSpeech,
		}
		for _, d := range m.Definitions {
			meaning.Definitions = append(meaning.Definitions, d.Definition)
			if d.Example != "" {
				res.Examples = append(res.Examples, d.Example)
			}
		}
		res.Meanings = append(res.Meanings, meaning)
	}
	return res
}
