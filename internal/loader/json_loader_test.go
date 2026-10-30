package loader

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileDynasty(t *testing.T) {
	assert.Equal(t, "宋", fileDynasty("poet.song.12000.json", "唐"))
	assert.Equal(t, "唐", fileDynasty("poet.tang.0.json", "唐"))
	assert.Equal(t, "宋", fileDynasty("ci.song.0.json", "宋"))
	assert.Equal(t, "唐", fileDynasty("唐诗三百首.json", "唐"), "files without a dynasty token keep the dataset's")
}

// writeJSON 把 v 写成 JSON 文件，目录不存在时一并创建。
func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	data, err := json.Marshal(v)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))
}

func TestLoadAllAssignsDynastyPerFileInStableOrder(t *testing.T) {
	root := t.TempDir()
	poem := func(title string) []map[string]any {
		return []map[string]any{{"title": title, "author": "某", "paragraphs": []string{"一二三四五。"}}}
	}
	writeJSON(t, filepath.Join(root, "全唐诗", "poet.song.0.json"), poem("宋诗"))
	writeJSON(t, filepath.Join(root, "全唐诗", "poet.tang.0.json"), poem("唐诗"))
	writeJSON(t, filepath.Join(root, "元曲", "yuanqu.json"), poem("元曲"))
	writeJSON(t, filepath.Join(root, "宋词", "ci.song.0.json"), poem("宋词"))

	// 数据集 ID 的顺序与 map 键的字典序、书写顺序都不同，以确认按 ID 排序
	writeJSON(t, filepath.Join(root, "loader", "datas.json"), map[string]any{
		"cp_path": "./",
		"datasets": map[string]any{
			"songci":   map[string]any{"name": "宋词", "id": 5, "path": "宋词/", "tag": "paragraphs"},
			"tangsong": map[string]any{"name": "全唐诗全宋诗", "id": 3, "path": "全唐诗/", "tag": "paragraphs"},
			"yuanqu":   map[string]any{"name": "元曲", "id": 2, "path": "元曲/yuanqu.json", "tag": "paragraphs"},
		},
	})

	l, err := NewJSONLoader(filepath.Join(root, "loader", "datas.json"))
	require.NoError(t, err)

	for range 5 { // map 遍历顺序随机，多跑几次
		poems, err := l.LoadAll()
		require.NoError(t, err)

		var got [][2]string
		for _, p := range poems {
			got = append(got, [2]string{p.Title, p.Dynasty})
		}
		assert.Equal(t, [][2]string{
			{"元曲", "元"},
			{"宋诗", "宋"}, // 全唐诗目录下的 poet.song.* 不再被判为唐
			{"唐诗", "唐"},
			{"宋词", "宋"},
		}, got)
	}
}

func TestLoadAuthorBios(t *testing.T) {
	root := t.TempDir()
	poem := []map[string]any{{"title": "t", "author": "某", "paragraphs": []string{"一二三。"}}}
	writeJSON(t, filepath.Join(root, "全唐诗", "poet.tang.0.json"), poem)
	writeJSON(t, filepath.Join(root, "全唐诗", "authors.tang.json"), []map[string]any{{"name": "李白", "desc": "李白，字太白。"}})
	writeJSON(t, filepath.Join(root, "全唐诗", "authors.song.json"), []map[string]any{{"name": "蘇軾", "desc": "蘇軾，字子瞻。"}, {"name": "無傳", "desc": ""}})
	writeJSON(t, filepath.Join(root, "全唐诗", "error", "authors.tang.json"), []map[string]any{{"name": "错", "desc": "子目录不读"}})
	writeJSON(t, filepath.Join(root, "宋词", "ci.song.0.json"), poem)
	writeJSON(t, filepath.Join(root, "宋词", "author.song.json"), []map[string]any{{"name": "苏轼", "description": "北宋文学家。", "short_description": "略"}})
	writeJSON(t, filepath.Join(root, "五代诗词", "nantang", "poetrys.json"), poem)
	writeJSON(t, filepath.Join(root, "五代诗词", "nantang", "authors.json"), []map[string]any{{"name": "李璟", "desc": "南唐元宗。"}})
	writeJSON(t, filepath.Join(root, "loader", "datas.json"), map[string]any{
		"cp_path": "./",
		"datasets": map[string]any{
			"wudai-nantang": map[string]any{"name": "五代-南唐", "id": 1, "path": "五代诗词/nantang/poetrys.json", "tag": "paragraphs"},
			"tangsong":      map[string]any{"name": "全唐诗全宋诗", "id": 3, "path": "全唐诗/", "tag": "paragraphs"},
			"songci":        map[string]any{"name": "宋词", "id": 5, "path": "宋词/", "tag": "paragraphs"},
		},
	})

	l, err := NewJSONLoader(filepath.Join(root, "loader", "datas.json"))
	require.NoError(t, err)
	bios, err := l.LoadAuthorBios()
	require.NoError(t, err)

	assert.Equal(t, []AuthorBio{
		{Name: "李璟", Description: "南唐元宗。", Dynasty: "五代"},
		{Name: "蘇軾", Description: "蘇軾，字子瞻。", Dynasty: "宋"}, // 全唐诗目录下的 authors.song.json 属于宋
		{Name: "李白", Description: "李白，字太白。", Dynasty: "唐"},
		{Name: "苏轼", Description: "北宋文学家。", Dynasty: "宋"},
	}, bios, "empty bios and subdirectories are skipped")
}

func TestInferDynastyFallsBackToPath(t *testing.T) {
	assert.Equal(t, "明", inferDynasty("ming", "明诗", "明/诗/poet.ming.json"))
	assert.Equal(t, "两汉", inferDynasty("han-shi", "汉诗（含文）", "汉/诗/poet.han.json"))
	assert.Equal(t, "魏晋", inferDynasty("jin-shi", "晋诗（含文）", "晋/诗/poet.jin.json"))
	assert.Equal(t, "五代", inferDynasty("wudai-shi", "五代十国诗", "五代十国/诗/poet.wudai.json"))
	assert.Equal(t, "当代", inferDynasty("dang-poet", "当代旧体诗", "当/poet.dang.json"))
	assert.Equal(t, "宋", inferDynasty("songshi", "全宋诗", "宋/诗/全宋诗/"))
	assert.Equal(t, "魏晋", inferDynasty("caocao", "曹操诗集", "汉/曹操诗集/caocao.json"), "explicit mapping wins over the path")
	assert.Equal(t, "其他", inferDynasty("guwen-guanzhi", "古文观止", "汇编/古文观止/"), "cross-dynasty anthologies stay undated")
}

func TestLoadAllCollectionAndStringParagraphs(t *testing.T) {
	root := t.TempDir()
	writeJSON(t, filepath.Join(root, "宋", "诗", "千家诗", "qianjiashi.json"), map[string]any{
		"title":  "千家詩",
		"author": "南宋謝枋得/明代王相",
		"paragraphs": []map[string]any{{
			"type": "七言絕句",
			"content": []map[string]any{
				{"chapter": "春日偶成", "author": "（宋）程顥", "paragraphs": []string{"雲淡風輕近午天，", "傍花隨柳過前川。"}},
				{"chapter": "攜妓納涼晚際遇雨", "author": "（唐）杜甫", "paragraphs": []map[string]any{
					{"subchapter": "其一", "paragraphs": []string{"落日放船好，", "輕風生浪遲。"}},
					{"subchapter": "其二", "paragraphs": []string{"雨來沾席上，", "風急打船頭。"}},
				}},
				{"chapter": "題壁", "author": "無名氏", "paragraphs": []string{"一團茅草亂蓬蓬。"}},
			},
		}},
	})
	// 幽梦影的 paragraphs 是单个字符串而不是数组
	writeJSON(t, filepath.Join(root, "清", "幽梦影", "youmengying.json"), []map[string]any{
		{"paragraphs": "读经宜冬，其神专也。", "comment": []string{"曹秋岳曰"}},
		{"paragraphs": ""},
	})
	writeJSON(t, filepath.Join(root, "loader", "datas.json"), map[string]any{
		"cp_path": "./",
		"datasets": map[string]any{
			"qianjiashi":  map[string]any{"name": "千家詩", "id": 1, "path": "宋/诗/千家诗/qianjiashi.json", "tag": "paragraphs"},
			"youmengying": map[string]any{"name": "幽梦影", "id": 2, "path": "清/幽梦影/youmengying.json", "tag": "paragraphs"},
		},
	})

	l, err := NewJSONLoader(filepath.Join(root, "loader", "datas.json"))
	require.NoError(t, err)
	poems, err := l.LoadAll()
	require.NoError(t, err)

	type row struct{ Title, Author, Dynasty string }
	var got []row
	for _, p := range poems {
		got = append(got, row{p.Title, p.Author, p.Dynasty})
	}
	assert.Equal(t, []row{
		{"春日偶成", "程顥", "宋"},
		{"攜妓納涼晚際遇雨·其一", "杜甫", "唐"},
		{"攜妓納涼晚際遇雨·其二", "杜甫", "唐"},
		{"題壁", "無名氏", "宋"}, // 没有朝代标注，沿用数据集的朝代
		{"", "张潮", "清"},
	}, got)
}
