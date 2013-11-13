package main

import (
    "os"
    "fmt"
    "flag"
    "log"
    "io/ioutil"
    "os/exec"
    "strings"
    // "path/filepath"
)


type Config struct {
    gitPath string
    gitName string
}

func (c *Config) GitPath() string {
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

var config = Config{}

var colors = map[string]string{
        "header": "\033[95m",
        "blue":   "\033[94m",
        "green":  "\033[92m",
        "yellow": "\033[93m",
        "red":    "\033[91m",
        "bold":   "\033[1m",
        "end":    "\033[0m",
}

func color(s string, color string) string {
        return fmt.Sprintf("%s%s%s", colors[color], s, colors["end"])
}

type Wart struct {
    Reporter string
    Line int
    Column int
    IssueCode string
    Message string
}

type UglyFile struct {
    Path string
    Warts []Wart
}


func get_dir_files(dir string) []os.FileInfo {
    entries, err := ioutil.ReadDir(dir)
    if err != nil {
        log.Fatal("Could not read directory", dir)
    }
    return entries
}

func gitBranchFiles() []os.FileInfo {
    dirtyFilesCmd := exec.Command("git", "diff", "--name-only")
    dirtyFiles, err := dirtyFilesCmd.Output()
    if err != nil {
        log.Fatal("Failed to list dirty files")
    }

    branchFilesCmd := exec.Command("git", "diff", "--name-only", "master..HEAD")
    branchFiles, err := branchFilesCmd.Output()
    if err != nil {
        log.Fatal("Failed to list branch files:", err)
    }

    byLines := append(
        strings.Split(string(dirtyFiles), "\n"),
        strings.Split(string(branchFiles), "\n")...
    )

    files := make([]os.FileInfo, len(byLines))
    for i, path := range byLines {
        info, err := os.Stat(path)
        if err != nil {
            log.Fatal("Failed accessing", path)
        }
        files[i] = info
    }
    return files
}

func main() {
    var branch bool
    var dir string
    flag.BoolVar(&branch, "b", false, "Run against current branch")
    flag.StringVar(&dir, "d", "", "Run against a directory")
    flag.Parse()

    fmt.Println("Branch", branch)
    fmt.Println("Dir", dir)

    var files []os.FileInfo
    if len(dir) > 0 {
        files = get_dir_files(dir)
        for _, entry := range files {
            log.Print("entry: ", entry)
        }
    } else if branch {
        log.Print("config.GitPath(): ", config.GitPath())
        gitBranchFiles()
    }

}

