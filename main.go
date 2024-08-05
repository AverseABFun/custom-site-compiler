package main

import (
	"crypto/sha1"
	"encoding/hex"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/averseabfun/logger"
	"github.com/russross/blackfriday/v2"
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

func removeConsecutiveDuplicates(s string) string {
	if len(s) == 0 {
		return s
	}

	var sb strings.Builder
	prevChar := rune(s[0])
	sb.WriteRune(prevChar)

	for _, char := range s[1:] {
		if char != prevChar {
			sb.WriteRune(char)
			prevChar = char
		}
	}

	return sb.String()
}

func parseCondition(condition string) bool {
	if !strings.Contains(condition, "=") {
		var notting = strings.HasPrefix(condition, "!")
		condition = strings.TrimPrefix(condition, "!")
		if strings.HasPrefix(condition, "build.") {
			return (notting && buildArgs[strings.TrimPrefix(condition, "build.")] != "true") || ((!notting) && buildArgs[strings.TrimPrefix(condition, "build.")] == "true")
		}
		return (notting && variables[condition] != "true") || (!notting && variables[condition] == "true")
	}
	condition = removeConsecutiveDuplicates(condition)
	var val = strings.Split(condition, "=")
	if len(val) > 2 {
		logger.Logf(logger.LogFatal, "Invalid number of sides to condition, expected two but got %d", val)
	}
	var notting = strings.HasSuffix(val[0], "!")
	if notting {
		val[0] = strings.TrimSuffix(val[0], "!")
	}

	var usingRawRightSide = strings.HasPrefix(val[1], "\"")
	if usingRawRightSide {
		val[1] = strings.Trim(val[1], "\"")
	}
	if strings.HasPrefix(val[0], "build.") {
		val[0] = buildArgs[strings.TrimPrefix(val[0], "build.")]
	} else {
		val[0] = variables[val[0]]
	}
	if strings.HasPrefix(val[1], "build.") && usingRawRightSide {
		val[1] = buildArgs[strings.TrimPrefix(val[1], "build.")]
	} else if usingRawRightSide {
		val[0] = variables[val[0]]
	}
	return (notting && val[0] != val[1]) || ((!notting) && val[0] == val[1])
}

var buildArgs = map[string]string{}
var variables = map[string]string{}
var objects = map[string]map[string]string{}

var depth = int64(0)
var depthLimit = flag.Int64("maximum-depth", 100, "Maximum depth limit")

func processStringForIfs(input string) string {
	var output strings.Builder
	lines := strings.Split(input, "\n")

	inCondition := false
	skipLines := false
	inMarkdown := false
	md := ""
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		line = strings.TrimSpace(line)

		if line == "<?markdown>" {
			inMarkdown = true
			continue
		}

		if line == "<?endmd>" && inMarkdown {
			logger.Log(logger.LogDebug, md)
			inMarkdown = false
			output.WriteString(mdToHTML(md))
			md = ""
			continue
		}

		if inMarkdown {
			md += line + "\n"
			continue
		}

		if strings.HasPrefix(line, "<?if ") {
			condition := strings.TrimPrefix(line, "<?if ")
			condition = strings.TrimSuffix(condition, ">")
			inCondition = parseCondition(condition)
			skipLines = !inCondition
		} else if strings.HasPrefix(line, "<?else>") {
			if !inCondition {
				skipLines = false
			} else {
				skipLines = !skipLines
			}
		} else if strings.HasPrefix(line, "<?endif>") {
			inCondition = false
			skipLines = false
		} else if strings.HasPrefix(line, "<?setvar ") && !skipLines {
			thing := strings.TrimPrefix(line, "<?setvar ")
			thing = strings.TrimSuffix(thing, ">")
			key := strings.Split(thing, "=")[0]
			val := strings.Split(thing, "=")[1]
			variables[key] = val
			logger.Logf(logger.LogDebug, "%s: %s", key, val)

			input = strings.ReplaceAll(input, "{{"+key+"}}", val)
			lines = strings.Split(input, "\n")
		} else {
			if !skipLines {
				output.WriteString(line + "\n")
			}
		}
	}

	return output.String()
}

func mdToHTML(markdown string) string {
	output := blackfriday.Run([]byte(markdown))
	return string(output)
}

func walkPath(path string, d fs.DirEntry, _ error) error {
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
		logger.Logf(logger.LogDebug, "%v", val)
		var keyword = val[1]
		var args = createArgsFromSlice(strings.Split(val[2], " "))
		switch keyword {
		case "static":
			var kind = filepath.Ext(args[0])
			var format = ""
			switch kind {
			case ".js":
				format = "<script src=\"{path}\"></script>"
			case ".css":
				format = "<link rel=\"stylesheet\" href=\"{path}\">"
			default:
				logger.Logf(logger.LogWarning, "Unsupported file type in static directive: %s", kind)
				stringData = strings.ReplaceAll(stringData, val[0], "")
				continue OuterRegexLoop
			}
			os.Mkdir(filepath.Join(outDir, "static"), 0700)
			var file, err = os.ReadFile(filepath.Join(filepath.Dir(path), args[0]))
			if os.IsNotExist(err) {
				logger.Logf(logger.LogWarning, "Path provided in static directive(%s) does not exist", args[0])
				stringData = strings.ReplaceAll(stringData, val[0], "")
				continue OuterRegexLoop
			}

			var hasher = sha1.New()
			var hash = hex.EncodeToString(hasher.Sum(file)[:8])

			args[1] = strings.ReplaceAll(args[1], "[hash]", hash)

			os.WriteFile(filepath.Join(outDir, "static", args[1]), file, 0700)

			logger.Logf(logger.LogDebug, "Replacing in stringData: %s -> %s", val[0], strings.ReplaceAll(format, "{path}", filepath.Join("static", args[1])))

			stringData = strings.ReplaceAll(stringData, val[0], strings.ReplaceAll(format, "{path}", filepath.Join("static", args[1])))

			logger.Logf(logger.LogDebug, "Current stringData: %s", stringData)
		case "include":
			args[0] = filepath.Join(filepath.Dir(path), args[0])
			info, err := os.Stat(args[0])
			if os.IsNotExist(err) {
				logger.Logf(logger.LogWarning, "Path provided in include directive(%s) does not exist", args[0])
				stringData = strings.ReplaceAll(stringData, val[0], "")
				continue OuterRegexLoop
			}
			if info.IsDir() {
				logger.Logf(logger.LogWarning, "Path provided in include directive(%s) not a file", args[0])
				stringData = strings.ReplaceAll(stringData, val[0], "")
				continue OuterRegexLoop
			}
			entry, _ := getDirEntry(args[0])
			depth++
			walkPath(args[0], entry, nil)
			depth--
			data, _ := os.ReadFile(outDir + strings.TrimSuffix(entry.Name(), ".hcsc") + ".html")
			strData := string(data)
			for _, val := range args[1:] {
				var splitted = strings.Split(val, "=")
				if len(splitted) != 2 {
					continue
				}
				strData = strings.ReplaceAll(strData, "{{"+splitted[0]+"}}", splitted[1])
			}

			stringData = strings.ReplaceAll(stringData, val[0], strData)
		}
	}

	stringData = processStringForIfs(stringData)
	logger.Log(logger.LogDebug, stringData)

	os.WriteFile(outDir+strings.TrimSuffix(d.Name(), ".hcsc")+".html", []byte(stringData), 0700)

	var f, _ = os.ReadFile(outDir + strings.TrimSuffix(d.Name(), ".hcsc") + ".html")
	logger.Logf(logger.LogDebug, "%s", string(f))

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
	if flag.Arg(1) != "" && !strings.HasPrefix(flag.Arg(1), "-build") {
		outDir = flag.Arg(1)
		if !strings.HasSuffix(outDir, "/") {
			outDir = outDir + "/"
		}
	}
	os.Mkdir(outDir, 0700)

	customFlagRegex := regexp.MustCompile(`^-build:([a-zA-Z0-9]+)(?:=([a-zA-Z0-9]+))?$`)

	for _, arg := range flag.Args() {
		if matches := customFlagRegex.FindStringSubmatch(arg); matches != nil {
			if matches[2] == "" {
				matches[2] = "true"
			}
			buildArgs[matches[1]] = matches[2]
		}
	}

	filepath.WalkDir(flag.Arg(0), walkPath)

	no_serve, _ := os.ReadFile(filepath.Join(flag.Arg(0), "no-serve.txt"))
	var real_no_serve = strings.Split(string(no_serve), "\n")

	for _, val := range real_no_serve {
		os.Remove(filepath.Join(outDir, strings.ReplaceAll(val, ".hcsc", "")+".html"))
	}

}
