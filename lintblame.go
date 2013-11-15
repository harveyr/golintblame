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
    "strconv"
    "strings"
    "time"
	// "sort"
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

type Config struct {
	BranchMode   bool
	WorkingDir   string
	ArgPath      string
	InitialPaths []string
	PrintLimit   int
}

var config = Config{}

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
	TimeMap map[string]time.Time
}

func (m *ModifiedTimes) CheckTime(path string) bool {
	hasChanged := false
	fileInfo, err := os.Stat(path)
	if err == nil {
		if fileInfo != nil {
			mt := fileInfo.ModTime()
			if storedTime, ok := m.TimeMap[path]; ok {
				if !storedTime.Equal(mt) {
					hasChanged = true
				}
			} else {
				hasChanged = true
			}
			if hasChanged {
				m.TimeMap[path] = mt
			}
		}
	}
	return hasChanged
}

// Make sure the most recent file is at the front of end list, so it's most visible in the output
func (m ModifiedTimes) SortaSorted() []string {
    returnSlice := make([]string, 0)
	var mostRecentTime time.Time
	for path, time := range m.TimeMap {
		if time.After(mostRecentTime) {
            // If more recent, append it
            returnSlice = append(returnSlice, path)
            mostRecentTime = time
        } else {
            returnSlice = append([]string{path}, returnSlice...)
        }
	}
    return returnSlice
}

func (m ModifiedTimes) Len() int {
	return len(m.TimeMap)
}

func NewModifiedTimes() *ModifiedTimes {
	modTimes := ModifiedTimes{TimeMap: make(map[string]time.Time)}
	for _, file := range targetPaths() {
		modTimes.CheckTime(file)
	}
	return &modTimes
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

func NewWart(reporter string, line string, column string, issueCode string, message string) Wart {

	line64, err := strconv.ParseInt(line, 10, 0)
	if err != nil {
		log.Fatalf("Failed parsing line number %s", line)
	}
	col64, err := strconv.ParseInt(column, 10, 0)
	if err != nil {
		log.Fatalf("Failed parsing column number %s", column)
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

type TargetFile struct {
	Path         string
	ContentLines []string
	BlameLines   []string
	Warts        map[int][]Wart
}

func (tf *TargetFile) Blame() {
	os.Chdir(config.WorkingDir)
	cmd := exec.Command("git", "blame", tf.Path)
	results, err := cmd.Output()
	if err != nil {
		tf.BlameLines = make([]string, 0)
	} else {
		tf.BlameLines = strings.Split(string(results), "\n")
	}
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

// Run a go command against the file. E.g., `go build`
func (tf *TargetFile) GoCmd(goCmd string) {
	if !tf.ExtEquals(".go") {
		return
	}
	os.Chdir(config.WorkingDir)
	_, file := filepath.Split(tf.Path)
	cmd := exec.Command("go", goCmd, file)
	results, _ := cmd.CombinedOutput()
	parsed := rexes["goBuild"].FindAllStringSubmatch(string(results), -1)
	for _, group := range parsed {
		wart := NewWart(goCmd, group[1], "0", "-", group[2])
		tf.AddWart(wart)
	}
}

// Run `go build`
func (tf *TargetFile) GoBuild() {
	tf.GoCmd("build")
}

// Run `go vet`
func (tf *TargetFile) GoVet() {
	tf.GoCmd("vet")
}

// Run `pylint`
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

// Get the blame name for a given line
func (tf TargetFile) BlameName(line int) string {
	if len(tf.BlameLines) == 0 {
		return "-"
	}
	match := rexes["blameName"].FindStringSubmatch(tf.BlameLines[line-1])
	return strings.TrimSpace(match[1])
}

// Create a TargetFile
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
	tf.GoVet()
	return &tf
}

// Create a TargetFile in a goroutine
func makeTargetFile(filepath string, c chan *TargetFile) {
	tf := NewTargetFile(filepath)
	c <- tf
}

// Returns paths to watch for a given directory
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

// Returns paths to watch for the current branch
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

// Filters candidate paths to those that should be watched
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
					filepath = path.Join(config.WorkingDir, filepath)
				}
				goodstuffs = append(goodstuffs, filepath)
			}
		}
	}
	return goodstuffs
}

// Print the target file's issues
func printWarts(targetFile *TargetFile) {
	if len(targetFile.Warts) == 0 {
		fmt.Printf(
			"%s [%s]",
			color("green", targetFile.Path),
			color("bold", "clean"),
		)
	} else {
		fmt.Println(color("yellow", targetFile.Path))
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

// Clear the screen and print the header
func clear() {
	cmd := exec.Command("clear")
	cmd.Stdout = os.Stdout
	cmd.Run()
	fmt.Println(
		fmt.Sprintf(
			"%s %s%s %s",
			color("bold", "---"),
			color("bold", "lint"),
			color("red", "blame"),
			color("bold", "---"),
		),
	)
}

func printResults(modTimes ModifiedTimes) {
    filepaths := modTimes.SortaSorted()
    log.Print("filepaths: ", filepaths)
	start := time.Now()
	c := make(chan *TargetFile)
	for _, path := range filepaths {
        go makeTargetFile(path, c)
	}
	cleared := false
	for i := 0; i < len(filepaths); i++ {
		if !cleared {
			// clear()
			cleared = true
		}
		tf := <-c
		printWarts(tf)
		fmt.Println("")
	}
	duration := time.Now().Sub(start)

	fmt.Printf(
		"[last ran at %d:%d:%d in %s]\n",
		start.Hour(),
		start.Minute(),
		start.Second(),
		duration,
	)

}

func getFileInfo(filepath string) os.FileInfo {
	fileInfo, err := os.Stat(filepath)
	if err != nil {
		log.Fatal("Failed to get info for path: ", filepath)
	}
	return fileInfo
}

// Returns path slice based on command line argument path
func argPathPaths() []string {
	fileInfo := getFileInfo(config.ArgPath)
	if fileInfo.IsDir() {
		return getDirFiles(config.ArgPath)
	} else {
		return filterFiles([]string{config.ArgPath})
	}
}

// Return the paths to be watched
func targetPaths() []string {
	if config.BranchMode {
		return gitBranchFiles()
	}
	return argPathPaths()
}

// init() runs when testing as well, so keep this named something else.
func initConfig() {
	var branch bool
	flag.BoolVar(&branch, "b", false, "Run against current branch")
	flag.Parse()

	config.BranchMode = branch

	if branch {
		config.WorkingDir = env.GitPath()
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
			config.ArgPath = absPath
			if stat.IsDir() {
				config.WorkingDir = absPath
			} else {
				dir, _ := filepath.Split(absPath)
				config.WorkingDir = dir
			}
		}
	}
	config.InitialPaths = targetPaths()
}

func main() {
    initConfig()
	filepaths := config.InitialPaths
	modTimes := NewModifiedTimes()
	printResults(*modTimes)
	loopCount := 0
	for {
		runUpdate := false
		for _, file := range filepaths {
			if modTimes.CheckTime(file) {
				runUpdate = true
				break
			}
		}
		if runUpdate {
			printResults(*modTimes)
		}
		if loopCount%5 == 0 {
			// Update file list
			oldLen := modTimes.Len()
			modTimes = NewModifiedTimes()
			if modTimes.Len() != oldLen {
				printResults(*modTimes)
			}
		}
		time.Sleep(1 * time.Second)
		loopCount += 1
	}
}
