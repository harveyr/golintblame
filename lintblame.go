package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var colors = map[string]string{
	"header": "\033[95m",
	"blue":   "\033[94m",
	"green":  "\033[92m",
	"yellow": "\033[93m",
	"red":    "\033[91m",
	"bold":   "\033[1m",
	"end":    "\033[0m",
}

func color(color string, s string) string {
	return fmt.Sprintf("%s%s%s", colors[color], s, colors["end"])
}

var rexes = map[string]*regexp.Regexp{
	"pep8":      regexp.MustCompile(`\w+:(\d+):(\d+):\s(\w+)\s(.+)(?m)$`),
	"pylint":    regexp.MustCompile(`(?m)^(\w):\s+(\d+),\s*(\d+):\s(.+)$`),
	"blameName": regexp.MustCompile(`\(([\w\s]+)\d{4}`),
	"goBuild":   regexp.MustCompile(`\w+:(\d+):\s(.+)(?m)$`),
}

var config = map[string]string{
	"workingDir": "",
}

type Environment struct {
	gitPath string
	gitName string
}

func (c *Environment) GitPath() string {
	if len(c.gitPath) == 0 {
		cmd := exec.Command("git", "rev-parse", "--show-toplevel")
		out, err := cmd.Output()
		if err != nil {
			log.Fatal("Failed to find git parent path.")
		}
		c.gitPath = strings.TrimSpace(string(out))
	}
	return c.gitPath
}

func (c *Environment) GitName() string {
	if len(c.gitName) == 0 {
		cmd := exec.Command("git", "config", "user.name")
		out, err := cmd.Output()
		if err != nil {
			c.gitName = "None"
		} else {
			c.gitName = strings.TrimSpace(string(out))
		}
	}
	return c.gitName
}

func (c Environment) CurrentGitBranch() string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		log.Fatal("Failed to get git branch")
	}
	return strings.TrimSpace(string(out))
}

var env = Environment{}

type Times []time.Time

func (s Times) Len() int      { return len(s) }
func (s Times) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

type ByTime struct{ Times }

func (s ByTime) Less(i, j int) bool { return s.Times[i].Before(s.Times[j]) }

type ModifiedTimes struct {
	timeMap map[string]time.Time
}

func (m *ModifiedTimes) CheckTime(path string) bool {
	hasChanged := false

	fileInfo, err := os.Stat(path)
	if err != nil {
		log.Println("Couldn't access ", path, ". Skipping it.")
	} else {
		if fileInfo != nil {
			mt := fileInfo.ModTime()
			if storedTime, ok := m.timeMap[path]; ok {
				if !storedTime.Equal(mt) {
					hasChanged = true
				}
			} else {
				hasChanged = true
			}
			if hasChanged {
				m.timeMap[path] = mt
			}
		}
	}
	return hasChanged
}

func (m ModifiedTimes) MostRecent() string {
	var returnPath string
	var mostRecentTime time.Time
	for path, time := range m.timeMap {
		if mostRecentTime.Before(time) {
			returnPath = path
			mostRecentTime = time
		}
	}
	return returnPath
}

func (m ModifiedTimes) PathsByModTime() []string {
	size := len(m.timeMap)
	times := make(Times, size)
	timesToPaths := make(map[time.Time]string, size)
	returnSlice := make([]string, size)
	i := 0
	for path, time_ := range m.timeMap {
		times[i] = time_
		timesToPaths[time_] = path
		i += 1
	}
	sort.Sort(ByTime{times})

	for i, time_ := range times {
		returnSlice[i] = timesToPaths[time_]
	}
	return returnSlice
}

func NewModifiedTimes() *ModifiedTimes {
	return &ModifiedTimes{timeMap: make(map[string]time.Time, 9)}
}

type Wart struct {
	Reporter  string
	Line      int
	Column    int
	IssueCode string
	Message   string
}

func (w Wart) String() string {
	return fmt.Sprintf("%d: [%s %s] %s", w.Line, w.Reporter, w.IssueCode, w.Message)
}

type TargetFile struct {
	Path         string
	ContentLines []string
	BlameLines   []string
	Warts        map[int][]Wart
}

func (tf *TargetFile) Blame() {
	os.Chdir(config["workingDir"])
	cmd := exec.Command("git", "blame", tf.Path)
	results, err := cmd.Output()
	if err != nil {
		tf.BlameLines = make([]string, 0)
	} else {
		tf.BlameLines = strings.Split(string(results), "\n")
	}
}

func NewWart(reporter string, line string, column string, issueCode string, message string) Wart {

	line64, err := strconv.ParseInt(line, 10, 0)
	if err != nil {
		log.Fatal("Failed parsing line number %s", line)
	}
	col64, err := strconv.ParseInt(column, 10, 0)
	if err != nil {
		log.Fatal("Failed parsing column number %s", column)
	}
	w := Wart{
		Reporter:  reporter,
		Line:      int(line64),
		Column:    int(col64),
		IssueCode: issueCode,
		Message:   message,
	}
	return w
}

func (tf TargetFile) ExtEquals(ext string) bool {
	return filepath.Ext(tf.Path) == ext
}

func (tf *TargetFile) AddWart(wart Wart) {
	if _, ok := tf.Warts[wart.Line]; !ok {
		tf.Warts[wart.Line] = make([]Wart, 0)
	}
	tf.Warts[wart.Line] = append(tf.Warts[wart.Line], wart)
}

func (tf *TargetFile) Pep8() {
	if filepath.Ext(tf.Path) != ".py" {
		return
	}
	cmd := exec.Command("pep8", tf.Path)
	results, _ := cmd.Output()
	parsed := rexes["pep8"].FindAllStringSubmatch(string(results), -1)
	for _, group := range parsed {
		wart := NewWart("PEP8", group[1], group[2], group[3], group[4])
		tf.AddWart(wart)
	}
}

func (tf *TargetFile) GoBuild() {
	if !tf.ExtEquals(".go") {
		return
	}
	os.Chdir(config["workingDir"])
	_, file := filepath.Split(tf.Path)
	cmd := exec.Command("go", "build", file)
	results, _ := cmd.CombinedOutput()
	parsed := rexes["goBuild"].FindAllStringSubmatch(string(results), -1)
	for _, group := range parsed {
		wart := NewWart("gobuild", group[1], "0", "-", group[2])
		tf.AddWart(wart)
	}
}

func (tf *TargetFile) PyLint() {
	if filepath.Ext(tf.Path) != ".py" {
		return
	}
	cmd := exec.Command("pylint", "--output-format=text", tf.Path)
	results, _ := cmd.Output()
	parsed := rexes["pylint"].FindAllStringSubmatch(string(results), -1)
	for _, group := range parsed {
		wart := NewWart("Pylint", group[2], group[3], group[1], group[4])
		tf.AddWart(wart)
	}
}

func (tf TargetFile) BlameName(line int) string {
	if len(tf.BlameLines) == 0 {
		return "-"
	}
	match := rexes["blameName"].FindStringSubmatch(tf.BlameLines[line-1])
	return strings.TrimSpace(match[1])
}

func NewTargetFile(path string) *TargetFile {
	tf := TargetFile{
		Path:  path,
		Warts: make(map[int][]Wart),
	}
	bytes, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatal("Unable to read file ", path)
	}
	tf.ContentLines = strings.Split(string(bytes), "\n")
	tf.Blame()
	tf.Pep8()
	tf.PyLint()
	tf.GoBuild()
	return &tf
}

func getDirFiles(dirPath string) []string {
	files, err := ioutil.ReadDir(dirPath)
	if err != nil {
		log.Fatal("Could not read directory", dirPath)
	}
	filepaths := make([]string, len(files))
	for i, fileInfo := range files {
		filepaths[i] = path.Join(dirPath, fileInfo.Name())
	}
	return filterFiles(filepaths)
}

func gitBranchFiles() []string {
	dirtyFilesCmd := exec.Command("git", "diff", "--name-only")
	dirtyFiles, err := dirtyFilesCmd.Output()
	if err != nil {
		log.Fatal("Failed to list dirty files")
	}

	branchFilesCmd := exec.Command("git", "diff", "--name-only", "master..HEAD")
	branchFiles, err := branchFilesCmd.Output()
	if err != nil {
		log.Print("branchFiles: ", branchFiles)
		log.Fatal("Failed to list branch files:", err)
	}

	allFiles := append(
		strings.Split(string(dirtyFiles), "\n"),
		strings.Split(string(branchFiles), "\n")...,
	)
	return filterFiles(allFiles)
}

func filterFiles(filepaths []string) []string {
	goodstuffs := make([]string, 0)
	for _, filepath := range filepaths {
		if len(filepath) > 0 {
			match, err := regexp.MatchString(".py|.go", path.Ext(filepath))
			if err != nil {
				log.Fatalf("Failed checking %s's extension", filepath)
			}
			if match == true {
				if !strings.HasPrefix(filepath, "/") {
					filepath = path.Join(config["workingDir"], filepath)
				}
				goodstuffs = append(goodstuffs, filepath)
			}
		}
	}
	return goodstuffs
}

func printWarts(targetFile *TargetFile) {
	fmt.Println(color("green", targetFile.Path))
	if len(targetFile.Warts) == 0 {
		fmt.Println(color("bold", "- All clean!"))
	}
	for line, warts := range targetFile.Warts {
		blameName := targetFile.BlameName(line)
		nameColor := "blue"
		if blameName == env.GitName() {
			nameColor = "yellow"
		}
		fmt.Printf(
			"%s: (%s) %s\n",
			color("bold", fmt.Sprintf("%d", line)),
			color(nameColor, blameName),
			strings.TrimSpace(targetFile.ContentLines[line-1]),
		)
		for _, wart := range warts {
			fmt.Printf(
				"    [%s %s] %s\n",
				wart.Reporter,
				wart.IssueCode,
				color("bold", wart.Message),
			)
		}
	}
}

func clear() {
	cmd := exec.Command("clear")
	cmd.Stdout = os.Stdout
	cmd.Run()
	fmt.Println(color("header", "--- LintBlame ---"))
}

func makeTargetFile(filepath string, c chan *TargetFile) {
	tf := NewTargetFile(filepath)
	c <- tf
}

func update(filepaths []string) {
	c := make(chan *TargetFile)
	for _, path := range filepaths {
		go makeTargetFile(path, c)
	}
	cleared := false
	for i := 0; i < len(filepaths); i++ {
		if !cleared {
			clear()
			cleared = true
		}
		tf := <-c
		printWarts(tf)
		fmt.Println("")
	}
}

func initialPaths() []string {
	var branch bool
	flag.BoolVar(&branch, "b", false, "Run against current branch")
	flag.Parse()

	var filepaths []string
	if branch {
		config["workingDir"] = env.GitPath()
		filepaths = gitBranchFiles()
	} else {
		args := flag.Args()
		if len(args) > 0 {
			target := args[0]
			stat, err := os.Stat(target)
			if err != nil {
				log.Fatal("Unable to process argument: ", target)
			}
			absPath, err := filepath.Abs(target)
			if err != nil {
				log.Fatal("Unable to get absolute path of ", target)
			}
			if stat.IsDir() {
				config["workingDir"] = absPath
				filepaths = getDirFiles(absPath)
			} else {
				dir, _ := filepath.Split(absPath)
				config["workingDir"] = dir
				filepaths = filterFiles([]string{absPath})
			}
		}
	}
	return filepaths
}

func main() {
	filepaths := initialPaths()
	modTimes := NewModifiedTimes()

	for _, file := range filepaths {
		modTimes.CheckTime(file)
	}
	update(modTimes.PathsByModTime())
	for {
		runUpdate := false
		for _, file := range filepaths {
			if modTimes.CheckTime(file) {
				runUpdate = true
				break
			}
		}
		if runUpdate {
			update(modTimes.PathsByModTime())
		}
		time.Sleep(1 * time.Second)
	}
}
