package loader

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// DataConfig 对应 datas.json 的结构。
type DataConfig struct {
	CPPath   string                 `json:"cp_path"`
	Datasets map[string]DatasetInfo `json:"datasets"`
}

// DatasetInfo 描述单个数据集的配置。
type DatasetInfo struct {
	Name     string   `json:"name"`
	ID       int      `json:"id"`
	Path     string   `json:"path"`
	Tag      string   `json:"tag"`
	Excludes []string `json:"excludes"`
	Comments string   `json:"comments,omitempty"`
}

// PoemData 是 JSON 文件中的一首诗词。
type PoemData struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Chapter    string   `json:"chapter,omitempty"` // 用于论语、四书五经
	Author     string   `json:"author"`
	Paragraphs []string `json:"paragraphs"`
	Rhythmic   string   `json:"rhythmic,omitempty"` // 词牌名，用于词
	Content    string   `json:"content,omitempty"`  // 正文的备用字段
	Para       []string `json:"para,omitempty"`     // 正文的备用字段

	dynasty string // 作品自带的朝代（如《千家诗》作者前的「（唐）」），为空则沿用数据集的朝代
}

// JSONLoader 从 JSON 文件中加载诗词数据。
type JSONLoader struct {
	config      *DataConfig
	basePath    string
	idToDynasty map[int]string
}

// NewJSONLoader 读取配置文件并创建加载器。
func NewJSONLoader(configPath string) (*JSONLoader, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config DataConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// 确定数据文件的根目录
	configDir := filepath.Dir(configPath)
	var basePath string

	// cp_path 指定了非当前目录时，与配置文件所在目录拼接
	if config.CPPath != "" && config.CPPath != "./" && config.CPPath != "." {
		basePath = filepath.Join(configDir, config.CPPath)
	} else {
		// cp_path 为 "./" 或 "." 时，根目录取 loader 目录的上一级
		basePath = filepath.Dir(configDir)
	}

	// 建立数据集 ID 到朝代的映射
	idToDynasty := make(map[int]string)
	for key, dataset := range config.Datasets {
		idToDynasty[dataset.ID] = inferDynasty(key, dataset.Name, dataset.Path)
	}

	return &JSONLoader{
		config:      &config,
		basePath:    basePath,
		idToDynasty: idToDynasty,
	}, nil
}

// LoadAll 加载全部数据集中的诗词数据。
//
// 数据集按 ID 顺序加载，而不是遍历 map：诗词 ID 按加载顺序分配（见 processor），
// map 的遍历顺序每次运行都不同，此前同一份数据每次发布都会得到一套不同的诗词 ID，
// 外部保存的 /poems/:id 链接随之失效。
func (l *JSONLoader) LoadAll() ([]PoemWithMeta, error) {
	var allPoems []PoemWithMeta

	for _, key := range l.sortedDatasetKeys() {
		dataset := l.config.Datasets[key]
		poems, err := l.loadDataset(key, dataset)
		if err != nil {
			return nil, fmt.Errorf("failed to load dataset %s: %w", key, err)
		}
		allPoems = append(allPoems, poems...)
	}

	return allPoems, nil
}

// sortedDatasetKeys 按数据集 ID（其次按 key）返回全部数据集的 key。
func (l *JSONLoader) sortedDatasetKeys() []string {
	keys := make([]string, 0, len(l.config.Datasets))
	for key := range l.config.Datasets {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b string) int {
		return cmp.Or(cmp.Compare(l.config.Datasets[a].ID, l.config.Datasets[b].ID), cmp.Compare(a, b))
	})
	return keys
}

// PoemWithMeta 是诗词数据加上其来源信息。
type PoemWithMeta struct {
	PoemData
	Dynasty     string
	DatasetName string
	DatasetKey  string
}

func (l *JSONLoader) loadDataset(key string, dataset DatasetInfo) ([]PoemWithMeta, error) {
	fullPath := filepath.Join(l.basePath, dataset.Path)
	dynasty := l.idToDynasty[dataset.ID]

	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat path %s: %w", fullPath, err)
	}

	var poems []PoemWithMeta

	if info.IsDir() {
		// 目录：逐个加载其中的 JSON 文件
		entries, err := os.ReadDir(fullPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read directory %s: %w", fullPath, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			// 跳过配置中排除的文件
			if contains(dataset.Excludes, entry.Name()) {
				continue
			}

			if filepath.Ext(entry.Name()) != ".json" {
				continue
			}

			filePath := filepath.Join(fullPath, entry.Name())
			filePoems, err := l.loadJSONFile(filePath, dataset.Tag)
			if err != nil {
				fmt.Printf("Warning: failed to load %s: %v\n", filePath, err)
				continue
			}

			fileDyn := fileDynasty(entry.Name(), dynasty)
			for _, poem := range filePoems {
				poemWithMeta := PoemWithMeta{
					PoemData:    poem,
					Dynasty:     cmp.Or(poem.dynasty, fileDyn),
					DatasetName: dataset.Name,
					DatasetKey:  key,
				}

				// 数据中没有作者时填入该数据集的默认作者
				if poemWithMeta.Author == "" {
					if defaultAuthor := getDefaultAuthorFromDataset(key); defaultAuthor != "" {
						poemWithMeta.Author = defaultAuthor
					}
				}

				poems = append(poems, poemWithMeta)
			}
		}
	} else {
		// 单个文件：直接加载
		filePoems, err := l.loadJSONFile(fullPath, dataset.Tag)
		if err != nil {
			return nil, err
		}

		for _, poem := range filePoems {
			poemWithMeta := PoemWithMeta{
				PoemData:    poem,
				Dynasty:     cmp.Or(poem.dynasty, dynasty),
				DatasetName: dataset.Name,
				DatasetKey:  key,
			}

			// 数据中没有作者时填入该数据集的默认作者
			if poemWithMeta.Author == "" {
				if defaultAuthor := getDefaultAuthorFromDataset(key); defaultAuthor != "" {
					poemWithMeta.Author = defaultAuthor
				}
			}

			poems = append(poems, poemWithMeta)
		}
	}

	return poems, nil
}

func (l *JSONLoader) loadJSONFile(path string, tag string) ([]PoemData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// 顶层是对象而不是数组的文件是嵌套的选本（《千家诗》）
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		return loadCollection(data)
	}

	var rawPoems []map[string]any
	if err := json.Unmarshal(data, &rawPoems); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	var poems []PoemData
	for _, raw := range rawPoems {
		poem := PoemData{
			Title:  getString(raw, "title"),
			Author: getString(raw, "author"),
		}

		// ID 字段
		if id, ok := raw["id"].(string); ok {
			poem.ID = id
		}

		// 词牌名，用于词
		if rhythmic, ok := raw["rhythmic"].(string); ok {
			poem.Rhythmic = rhythmic
		}

		// 章节名，用于论语、四书五经
		if chapter, ok := raw["chapter"].(string); ok {
			poem.Chapter = chapter
		}

		// 按配置的 tag 提取正文
		switch tag {
		case "paragraphs":
			poem.Paragraphs = getParagraphs(raw, "paragraphs")
		case "content":
			if content, ok := raw["content"].(string); ok {
				poem.Content = content
				poem.Paragraphs = []string{content}
			} else {
				poem.Paragraphs = getStringArray(raw, "content")
			}
		case "para":
			poem.Paragraphs = getParagraphs(raw, "para")
		default:
			// 未指定则依次尝试各个可能的字段
			if paras := getParagraphs(raw, "paragraphs"); len(paras) > 0 {
				poem.Paragraphs = paras
			} else if paras := getParagraphs(raw, "para"); len(paras) > 0 {
				poem.Paragraphs = paras
			} else if content, ok := raw["content"].(string); ok {
				poem.Paragraphs = []string{content}
			}
		}

		if len(poem.Paragraphs) > 0 {
			poems = append(poems, poem)
		}
	}

	return poems, nil
}

// dynastyByName 把朝代目录名（先秦、汉、五代十国……）与作者前的朝代标注（唐、南宋……）
// 归到数据库里的朝代名，无法识别时返回空。汉、三国、晋沿用库里已有的「两汉」「魏晋」；
// 辽金与宋同归「宋」，与 dynasties.json 的说明一致。
func dynastyByName(label string) string {
	switch label {
	case "先秦", "南北朝", "隋", "唐", "元", "明", "清", "宋", "五代", "两汉", "魏晋", "近现代", "当代":
		return label
	case "汉":
		return "两汉"
	case "三国", "晋":
		return "魏晋"
	case "五代十国":
		return "五代"
	case "南宋", "北宋", "辽", "金":
		return "宋"
	case "近现":
		return "近现代"
	case "当":
		return "当代"
	}
	return ""
}

// dynastyFromPath 按数据集路径的第一级目录判断朝代；汇编/ 这类跨代目录返回空。
func dynastyFromPath(path string) string {
	top, _, _ := strings.Cut(filepath.ToSlash(path), "/")
	return dynastyByName(top)
}

func inferDynasty(key, name, path string) string {
	// 数据集 key 到朝代的映射
	dynastyMap := map[string]string{
		"tangsong":          "唐",
		"songci":            "宋",
		"yuanqu":            "元",
		"wudai-huajianji":   "五代",
		"wudai-nantang":     "五代",
		"yudingquantangshi": "唐",
		"shuimotangshi":     "唐",
		"shijing":           "先秦",
		"chuci":             "先秦",
		"lunyu":             "先秦",
		"mengzi":            "先秦",
		"caocao":            "魏晋",
		"nalanxingde":       "清",
		"youmengying":       "清", // 张潮《幽梦影》
	}

	if dynasty, ok := dynastyMap[key]; ok {
		return dynasty
	}

	// 映射表中没有则尝试从名称推断
	if contains([]string{"唐"}, name) {
		return "唐"
	}
	if contains([]string{"宋"}, name) {
		return "宋"
	}
	if contains([]string{"元"}, name) {
		return "元"
	}

	// 数据集按朝代分目录，新增的数据集不必逐个登记到上面的映射表
	if dynasty := dynastyFromPath(path); dynasty != "" {
		return dynasty
	}

	return "其他"
}

// collectionFile 是《千家诗》这类嵌套选本：顶层是书名与作者，
// paragraphs 下按体裁分组，各组的 content 里逐首列出诗。
type collectionFile struct {
	Paragraphs []struct {
		// Author 形如「（唐）孟浩然」；Paragraphs 是字符串数组，或带 subchapter 的若干组
		Content []struct {
			Chapter    string          `json:"chapter"`
			Author     string          `json:"author"`
			Paragraphs json.RawMessage `json:"paragraphs"`
		} `json:"content"`
	} `json:"paragraphs"`
}

// loadCollection 把嵌套选本展平成一首一条；带 subchapter（其一、其二）的拆成各自独立的诗。
func loadCollection(data []byte) ([]PoemData, error) {
	var file collectionFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	var poems []PoemData
	for _, group := range file.Paragraphs {
		for _, item := range group.Content {
			dynasty, author := splitAuthorDynasty(item.Author)
			add := func(title string, paragraphs []string) {
				if len(paragraphs) > 0 {
					poems = append(poems, PoemData{Title: title, Author: author, Paragraphs: paragraphs, dynasty: dynasty})
				}
			}

			var lines []string
			if err := json.Unmarshal(item.Paragraphs, &lines); err == nil {
				add(item.Chapter, lines)
				continue
			}

			var parts []struct {
				Subchapter string   `json:"subchapter"`
				Paragraphs []string `json:"paragraphs"`
			}
			if err := json.Unmarshal(item.Paragraphs, &parts); err != nil {
				return nil, fmt.Errorf("unsupported paragraphs in %q: %w", item.Chapter, err)
			}
			for _, part := range parts {
				title := item.Chapter
				if part.Subchapter != "" {
					title += "·" + part.Subchapter
				}
				add(title, part.Paragraphs)
			}
		}
	}

	return poems, nil
}

// splitAuthorDynasty 拆开「（唐）孟浩然」，朝代无法识别或没有标注时返回空朝代。
func splitAuthorDynasty(author string) (dynasty, name string) {
	rest, ok := strings.CutPrefix(author, "（")
	if !ok {
		return "", author
	}
	label, name, ok := strings.Cut(rest, "）")
	if !ok {
		return "", author
	}
	return dynastyByName(label), strings.TrimSpace(name)
}

// AuthorBio 是作者文件中的一条诗人小传。
type AuthorBio struct {
	Name        string
	Description string
	Dynasty     string
}

// LoadAuthorBios 加载各数据集目录下作者文件（authors.tang.json、author.song.json、
// authors.json 等）中的诗人小传，按数据集 ID 与文件名排序，结果稳定。
//
// 作者文件与诗词文件放在一起，在 datas.json 中被列进 excludes，不会被当作诗词加载。
// 朝代与诗词一样按文件名修正（全唐诗目录下的 authors.song.json 属于宋）。
// 小传字段在不同文件中分别叫 desc 与 description。
func (l *JSONLoader) LoadAuthorBios() ([]AuthorBio, error) {
	type source struct {
		path    string
		dynasty string
	}
	var sources []source
	seenDirs := map[string]bool{}

	for _, key := range l.sortedDatasetKeys() {
		dataset := l.config.Datasets[key]
		dir := filepath.Join(l.basePath, dataset.Path)
		if info, err := os.Stat(dir); err != nil {
			return nil, fmt.Errorf("failed to stat path %s: %w", dir, err)
		} else if !info.IsDir() {
			dir = filepath.Dir(dir) // 单文件数据集（如南唐）的作者文件在同一目录下
		}
		if seenDirs[dir] {
			continue
		}
		seenDirs[dir] = true

		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("failed to read directory %s: %w", dir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasPrefix(name, "author") || filepath.Ext(name) != ".json" {
				continue
			}
			sources = append(sources, source{
				path:    filepath.Join(dir, name),
				dynasty: fileDynasty(name, l.idToDynasty[dataset.ID]),
			})
		}
	}

	var bios []AuthorBio
	for _, src := range sources {
		data, err := os.ReadFile(src.path)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", src.path, err)
		}
		var raw []map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", src.path, err)
		}
		for _, item := range raw {
			desc := getString(item, "desc")
			if desc == "" {
				desc = getString(item, "description")
			}
			name := getString(item, "name")
			if name == "" || strings.TrimSpace(desc) == "" {
				continue
			}
			bios = append(bios, AuthorBio{Name: name, Description: desc, Dynasty: src.dynasty})
		}
	}
	return bios, nil
}

// fileDynasty 依据文件名修正数据集级别的朝代。
//
// 「全唐诗」目录（数据集 tangsong，「全唐诗全宋诗」）同时收了唐诗与宋诗，
// 两者只靠文件名区分：poet.tang.N.json 与 poet.song.N.json。按数据集整体
// 判为唐时，约 25 万首宋诗都被标成了唐诗。
func fileDynasty(fileName, datasetDynasty string) string {
	switch {
	case strings.Contains(fileName, ".song."):
		return "宋"
	case strings.Contains(fileName, ".tang."):
		return "唐"
	default:
		return datasetDynasty
	}
}

// getDefaultAuthorFromDataset 返回数据集的默认作者，用于数据中缺少 author 字段的情况。
func getDefaultAuthorFromDataset(datasetKey string) string {
	authorMap := map[string]string{
		"caocao":      "曹操",
		"nalanxingde": "纳兰性德",
		"youmengying": "张潮", // 源数据只有 content 与 comment，没有作者字段
	}

	if author, ok := authorMap[datasetKey]; ok {
		return author
	}
	return ""
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// getParagraphs 取正文：多数数据是字符串数组，《幽梦影》等是单个字符串。
func getParagraphs(m map[string]any, key string) []string {
	if s, ok := m[key].(string); ok {
		if s == "" {
			return nil
		}
		return []string{s}
	}
	return getStringArray(m, key)
}

func getStringArray(m map[string]any, key string) []string {
	if arr, ok := m[key].([]any); ok {
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	}
	return nil
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
