package compose

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"
)

type testContext struct {
	built   bool
	pulled  bool
	ip      []byte
	logFile *os.File
	testNum int
}

var context testContext

// RunTest executes the passed function using Compose
func RunTest(t *testing.T, port string, testFunc func([]byte)) {
	if testing.Short() {
		t.Skip("skipping end-to-end test in short mode.")
	}

	// build project if not yet built
	if !context.built {
		if err := exec.Command("make").Run(); err != nil {
			t.Fatal("make failed: ", err)
		}
		context.built = true
	}

	// get Docker IP and cache it
	if len(context.ip) == 0 {
		dkm, err := exec.Command("docker-machine", "active").Output()
		if err == nil { // active Docker Machine detected, use it
			byteIP, err := exec.Command("docker-machine", "ip", string(bytes.TrimSpace(dkm))).Output()
			if err != nil {
				t.Fatal(err)
			}
			context.ip = bytes.TrimSpace(byteIP)
		} else { // no active docker machine, assume Docker is running natively
			context.ip = []byte("127.0.0.1")
		}
	}

	// log Compose output
	// TODO timestamps
	if context.logFile == nil {
		context.logFile, _ = os.Create("test.log")
	}
	defer func() {
		if err := context.logFile.Sync(); err != nil {
			t.Fatal("error syncing logfile: ", err)
		}
	}()

	// pull images if not yet pulled
	if !context.pulled {
		context.logFile.WriteString("pulling Compose images...")
		if out, err := exec.Command("docker-compose", "pull").CombinedOutput(); err != nil {
			context.logFile.Write(out)
			t.Fatal("error pulling Compose images: ", err)
		}
		context.pulled = true
		context.logFile.WriteString("done\n")
	}

	// bring up Compose
	cmd := exec.Command("docker-compose", "up", "--force-recreate", "--no-color")
	cmd.Stdout = context.logFile
	cmd.Stderr = context.logFile
	if err := cmd.Start(); err != nil {
		t.Fatal("error starting Compose: ", err)
	}
	defer func() {
		// send interrupt signal
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatal("error exiting Compose: ", err)
		}

		// wait for Compose to exit
		done := make(chan error, 1)
		go func() {
			done <- cmd.Wait()
		}()
		select {
		case <-time.After(5 * time.Second):
			// kill if it times out
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal("failed to kill compose: ", err)
			}
			t.Fatal("Compose killed as timeout reached")
		case err := <-done:
			if err != nil {
				t.Log("Compose exited with error: ", err)
			}
		}
		if out, err := exec.Command("docker-compose", "rm", "-f").CombinedOutput(); err != nil {
			t.Fatal("error removing containers: ", out)
		}
	}()

	// record test sequence number
	context.testNum++
	context.logFile.WriteString(fmt.Sprintf("--- test %v start\n", context.testNum))
	defer func() {
		time.Sleep(time.Millisecond * 100) // Ugly hack to stop log output from being cut off prematurely

		context.logFile.WriteString(fmt.Sprintf("--- test %v end\n", context.testNum))
	}()

	// poll until server is healthy
	start := time.Now()
	for func() bool {
		// TODO don't pass in port. either infer it or use context.
		resp, err := http.Head(fmt.Sprintf("http://%s%v/health_check", context.ip, port))
		if err == nil && resp.StatusCode == 204 {
			return false
		}
		return true
	}() {
		if time.Now().Sub(start) > time.Second*30 {
			t.Fatal("timed out waiting for server to start.")
		}
		time.Sleep(time.Millisecond * 250)
	}

	// Run test
	testFunc(context.ip)
}
