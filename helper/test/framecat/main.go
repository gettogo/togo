// Command framecat converts between newline-separated JSON and the helper
// protocol's length-prefixed frames, so shell pipelines can talk to
// "togo serve --stdio". By default it reads one JSON object per line on
// stdin and writes each as a frame; with -decode it reads frames and prints
// one JSON object per line.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/gettogo/togo/helper/internal/rpc"
)

func main() {
	decode := flag.Bool("decode", false, "read frames from stdin and print one JSON object per line")
	flag.Parse()
	var err error
	if *decode {
		err = decodeFrames(os.Stdin, os.Stdout)
	} else {
		err = encodeLines(os.Stdin, os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "framecat:", err)
		os.Exit(1)
	}
}

func encodeLines(r io.Reader, w io.Writer) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), rpc.MaxFrame)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := rpc.WriteFrame(w, line); err != nil {
			return err
		}
	}
	return sc.Err()
}

func decodeFrames(r io.Reader, w io.Writer) error {
	br := bufio.NewReader(r)
	for {
		payload, err := rpc.ReadFrame(br)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "%s\n", payload); err != nil {
			return err
		}
	}
}
