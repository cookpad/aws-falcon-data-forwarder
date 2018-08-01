// +build mage

package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/magefile/mage/mg" // mg contains helpful utility functions, like Deps
	"github.com/pkg/errors"
)

type buildTarget struct {
	name string
	path string
}

var (
	buildTargets = []buildTarget{
		{name: "receiver", path: "./functions/receiver"},
	}
)

// Building binaries
func Build() error {
	for _, target := range buildTargets {
		fmt.Println("Bulding ", target.name)
		cmd := exec.Command("go", "build",
			"-o", "build/"+target.name, target.path)
		cmd.Env = append(os.Environ(), "GOARCH=amd64", "GOOS=linux")

		err := cmd.Run()
		if err != nil {
			return err
		}
	}

	return nil
}

// Run test of each handler

func doTest(path string) error {
	cmd := exec.Command("go", "test", path, "-v")
	out, err := cmd.CombinedOutput()
	fmt.Printf(string(out))
	return err
}

func Test() error {
	fmt.Println("Testing...")

	for _, target := range buildTargets {
		err := doTest("./" + target.path)
		if err != nil {
			return err
		}
	}
	return nil
}

type config struct {
	StackName    string
	CodeS3Bucket string
	CodeS3Prefix string
	CodeS3Region string
	Parameters   []string
}

func loadConfigFile(fpath string) (config, error) {
	cfg := config{}
	cfg.Parameters = []string{}

	fp, err := os.Open(fpath)
	if err != nil {
		return cfg, err
	}
	defer fp.Close()

	scanner := bufio.NewScanner(fp)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 {
			continue
		}

		idx := strings.Index(line, "=")
		if idx < 0 {
			log.Printf("Warning, invalid format of cfg file: '%s'\n", line)
			continue
		}

		key := line[:idx]
		value := line[(idx + 1):]

		switch key {
		case "StackName":
			cfg.StackName = value
		case "CodeS3Bucket":
			cfg.CodeS3Bucket = value
		case "CodeS3Prefix":
			cfg.CodeS3Prefix = value
		case "CodeS3Region":
			cfg.CodeS3Region = value
		default:
			cfg.Parameters = append(cfg.Parameters, line)
		}
	}

	return cfg, nil
}

func deployCFn(paramFile string) error {

	cfg, err := loadConfigFile(paramFile)
	if err != nil {
		return err
	}

	templateFile := "template.yml"

	var tmpPath string
	if tf, err := ioutil.TempFile("", "slam_template_"); err != nil {
		log.Fatal(err)
	} else {
		tmpPath = tf.Name()
		tf.Close()
	}

	log.Printf("[%s] Packaging...\n", paramFile)
	pkgCmd := exec.Command("aws", "cloudformation", "package",
		"--template-file", templateFile,
		"--s3-bucket", cfg.CodeS3Bucket,
		"--s3-prefix", cfg.CodeS3Prefix,
		"--output-template-file", tmpPath)

	pkgOut, err := pkgCmd.CombinedOutput()
	if err != nil {
		log.Printf("[%s] Error: %s, %s", paramFile, string(pkgOut), err)
		return err
	}
	log.Printf("[%s] Generated template file: %s\n", paramFile, tmpPath)

	// fmt.Printf("Package > %s", string(pkgOut))
	log.Printf("[%s] Deploy...\n", paramFile)
	args := []string{
		"--region", cfg.CodeS3Region,
		"cloudformation", "deploy",
		"--template-file", tmpPath,
		"--stack-name", cfg.StackName,
		"--capabilities", "CAPABILITY_IAM",
		"--parameter-overrides",
	}
	args = append(args, cfg.Parameters...)
	deployCmd := exec.Command("aws", args...)

	deployOut, err := deployCmd.CombinedOutput()
	if err != nil {
		log.Println("[%s] Error: %s, %s", paramFile, string(deployOut), err)
		return err
	}

	log.Printf("[%s] Done!", paramFile)

	return nil
}

// Deploying CloudFormation stack
func Deploy() error {
	mg.Deps(Build)

	configFile := os.Getenv("PARAM_FILE")
	configDir := os.Getenv("PARAM_DIR")
	if configFile != "" {
		err := deployCFn(configFile)
		if err != nil {
			return err
		}
	} else if configDir != "" {
		files, err := ioutil.ReadDir(configDir)
		if err != nil {
			return errors.Wrap(err, "Fail to retrieve files in PARAM_DIR")
		}

		var wg sync.WaitGroup
		for _, finfo := range files {
			fpath := filepath.Join(configDir, finfo.Name())
			if !strings.HasSuffix(fpath, ".cfg") || finfo.IsDir() {
				continue
			}

			wg.Add(1)
			go func(fname string) {
				defer wg.Done()
				err := deployCFn(fname)
				if err != nil {
					log.Printf("[%s] ERROR %s", fname, err)
				}
			}(fpath)
		}

		wg.Wait()

	} else {
		return errors.New("PARAM_FILE is not available. Set PARAM_FILE as environment variable.")
	}

	return nil
}

// Remove all built binaries
func Clean() error {
	for _, target := range buildTargets {
		err := os.RemoveAll("config/build/" + target.name)
		if err != nil {
			return err
		}
	}
	return nil
}
