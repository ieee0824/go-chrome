package chrome

import (
	"github.com/joho/godotenv"
	"os"
	"path/filepath"
	"fmt"
	"github.com/ieee0824/getenv"
	"io/ioutil"
	"net"
	"runtime"
	"github.com/wirepair/gcd"
	"os/exec"
	"time"
	"io"
	"strings"
	"log"
)

var (
	USE_DOCKER_CHROME bool
	DEFAULT_USER_AGENT string
	DUMMY_RUN_SCRIPT_PATH string
	CHROME_PATH string
	USER_DIRECTORY string
)

func init(){
	current, err := filepath.Abs(".")
	if err != nil {
		panic(err)
	}
	godotenv.Load("~/.gochromerc")
	godotenv.Load(fmt.Sprintf("%s/%s", current, ".gochromerc"))
	godotenv.Load(fmt.Sprintf("%s/%s", current, ".env"))

	USE_DOCKER_CHROME = getenv.Bool("USE_DOCKER_CHROME", true)
	DEFAULT_USER_AGENT = getenv.String("DEFAULT_USER_AGENT", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.11; rv:55.0) Gecko/20100101 Firefox/55.0")

	if err := generateDummyRunScript(); err != nil {
		panic(err)
	}

	switch runtime.GOOS {
	case "windows":
		CHROME_PATH = getenv.String("CHROME_PATH", "C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe")
		USER_DIRECTORY = getenv.String("USER_DIRECTORY", "C:\\temp\\")
	case "darwin":
		CHROME_PATH = getenv.String("CHROME_PATH", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")
		USER_DIRECTORY = getenv.String("USER_DIRECTORY", "/tmp/")
	case "linux":
		CHROME_PATH = getenv.String("CHROME_PATH", "/usr/bin/chromium-browser")
		USER_DIRECTORY = getenv.String("USER_DIRECTORY", "/tmp/")
	}
}

func generateDummyRunScript()error{
	f, err := ioutil.TempFile(os.TempDir(), "go_chrome")
	if err != nil {
		return err
	}

	if err := os.Chmod(f.Name(), 0744); err != nil {
		return err
	}
	if _, err := f.Write([]byte("#!/bin/bash\n")); err != nil {
		return err
	}
	DUMMY_RUN_SCRIPT_PATH = f.Name()
	return nil
}

func getPort() int {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		panic(err)
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		panic(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

type Chrome struct {
	UserAgent string
	remotePort *int
	debugger *gcd.Gcd
	chromeContainerID string
}

func New()*Chrome{
	return &Chrome{
		UserAgent: DEFAULT_USER_AGENT,
	}
}

func (c *Chrome) startDockerChrome() error {
	out, err := exec.Command(
		"docker",
		"run",
		"-d",
		"--rm",
		"--privileged",
		"-p",
		fmt.Sprintf("%v:%v", *c.remotePort, *c.remotePort),
		"ieee0824/chrome:latest",
		fmt.Sprintf("--user-agent=%v", c.UserAgent),
		fmt.Sprintf("--remote-debugging-port=%v", *c.remotePort),
	).Output()
	if err != nil {
		return err
	}
	c.chromeContainerID = string(out)
	return nil
}

func (c *Chrome) startChrome() error {
	debugger := gcd.NewChromeDebugger()
	port := getPort()
	c.remotePort = &port

	if USE_DOCKER_CHROME {
		err := c.startDockerChrome()
		if err != nil {
			return err
		}
		debugger.StartProcess(DUMMY_RUN_SCRIPT_PATH, USER_DIRECTORY, fmt.Sprintf("%v", port))
	} else {
		debugger.StartProcess(CHROME_PATH, USER_DIRECTORY, fmt.Sprintf("%v", port))
	}

	c.debugger = debugger
	c.remotePort = &port

	return nil
}

func (c *Chrome) stopChrome() error {
	if USE_DOCKER_CHROME {
		fmt.Println(c.chromeContainerID)
		err := exec.Command(
			"docker",
			"kill",
			c.chromeContainerID[:12],
		).Run()
		if err != nil {
			log.Println(err)
		}
		c.debugger = nil
		c.chromeContainerID = ""
		c.remotePort = nil
	} else {
		if err := c.debugger.ExitProcess(); err != nil {
			log.Println()
			return err
		}
		c.debugger = nil
		c.remotePort = nil
	}
	return nil
}

func (c *Chrome) SetUserAgent(ua string) *Chrome {
	c.UserAgent = ua
	return c
}

func (c *Chrome) GetUserAgent() string {
	return c.UserAgent
}

func (c *Chrome) Get(u string) (r io.Reader, err error) {
	if err != c.startChrome() {
		log.Println()
		return nil, err
	}
	target, err := c.debugger.NewTab()
	if err != nil {
		log.Println()
		return nil, err
	}
	page := target.Page
	page.Navigate(u, "")
	page.Enable()
	time.Sleep(1 * time.Second)

	dom := target.DOM
	dom.GetDocument(-1, true)

	h, err := dom.GetOuterHTML(1)
	if err != nil {
		log.Println()
		return nil, err
	}
	if err := c.debugger.CloseTab(target); err != nil {
		log.Println()
		return nil, err
	}
	if err := c.stopChrome(); err != nil {
		log.Println()
		return nil, err
	}

	return strings.NewReader(h), nil
}
