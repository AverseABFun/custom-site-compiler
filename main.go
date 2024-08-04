package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/averseabfun/logger"
)

var outDir = "out/"
var keywordsRegex = regexp.MustCompile(`(?m)<\?(?P<keyword>.+?) (?P<args>.*?)>`)

func createArgsFromSlice(args []string) []string {
	var out = []string{}
	var inString = false
	var str = ""
	for _, val := range args {
		if !inString && strings.HasPrefix(val, "\"") {
			inString = true
		}
		if inString {
			str += strings.TrimPrefix(strings.TrimSuffix(val, "\""), "\"")
		} else {
			out = append(out, val)
		}
		if inString && strings.HasSuffix(val, "\"") {
			inString = false
			out = append(out, str)
			str = ""
		}
	}
	return out
}

func getDirEntry(path string) (fs.DirEntry, error) {
	// Get the directory and file name from the path
	dirPath, fileName := filepath.Split(path)

	// Open the directory
	dirEntries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	// Find the file in the directory entries
	for _, entry := range dirEntries {
		if entry.Name() == fileName {
			return entry, nil
		}
	}

	return nil, fmt.Errorf("file %s not found in directory %s", fileName, dirPath)
}

var depth = int64(0)
var depthLimit = flag.Int64("maximum-depth", 100, "Maximum depth limit")

func walkPath(path string, d fs.DirEntry, err error) error {
	if depth >= *depthLimit {
		logger.Logf(logger.LogFatal, "Reached depth limit of %d! There is probably a recursive include somewhere in your templates.", depthLimit)
	}
	if d.IsDir() {
		return nil
	}
	if !strings.HasSuffix(path, ".hcsc") {
		return nil
	}
	var data, er = os.ReadFile(path)
	if er != nil {
		return er
	}
	var stringData = string(data)

OuterRegexLoop:
	for _, val := range keywordsRegex.FindAllStringSubmatch(stringData, -1) {
		var keyword = val[keywordsRegex.SubexpIndex("keyword")]
		var args = createArgsFromSlice(strings.Split(val[keywordsRegex.SubexpIndex("args")], " "))
		switch keyword {
		case "include":
			if len(args) != 1 {
				logger.Logf(logger.LogWarning, "In file %s, got include directive with %d arguments, but expected only 1! Ignoring directive. The arguments are %v", path, len(args), args)
				stringData = strings.ReplaceAll(stringData, val[0], "")
				break OuterRegexLoop
			}
			args[0] = filepath.Join(filepath.Dir(path), args[0])
			info, err := os.Stat(args[0])
			if os.IsNotExist(err) {
				logger.Logf(logger.LogWarning, "Path provided in include directive(%s) does not exist", args[0])
				stringData = strings.ReplaceAll(stringData, val[0], "")
				break OuterRegexLoop
			}
			if info.IsDir() {
				logger.Logf(logger.LogWarning, "Path provided in include directive(%s) not a file", args[0])
				stringData = strings.ReplaceAll(stringData, val[0], "")
				break OuterRegexLoop
			}
			entry, _ := getDirEntry(args[0])
			depth++
			walkPath(args[0], entry, nil)
			depth--
			data, _ := os.ReadFile(args[0])

			stringData = strings.ReplaceAll(stringData, val[0], string(data))
		}
	}

	os.WriteFile(outDir+strings.TrimSuffix(d.Name(), ".hcsc")+".html", []byte(stringData), 0700)

	return nil
}

func main() {
	logger.Log(logger.LogInfo, "Averse's custom site compiler running...")
	flag.Parse()

	info, err := os.Stat(flag.Arg(0))
	if os.IsNotExist(err) {
		logger.Log(logger.LogFatal, "Path provided does not exist")
	}
	if !info.IsDir() {
		logger.Log(logger.LogFatal, "Path provided is not a directory")
	}
	if flag.Arg(1) != "" {
		outDir = flag.Arg(1)
		if !strings.HasSuffix(outDir, "/") {
			outDir = outDir + "/"
		}
	}
	os.Mkdir(outDir, 0700)

	filepath.WalkDir(flag.Arg(0), walkPath)
}
