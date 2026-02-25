package main

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"log"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

type ProxyWalkDirFunc func(path string, d ProxyDirEntry, err error) error

type ProxyDirEntry interface {
	fs.DirEntry
}

type Proxy struct {
	args     []string
	expected [][]string
	out      *os.File
}

func (p *Proxy) Chdir(dir string) {
	err := os.Chdir(dir)
	if err != nil {
		log.Fatal(err)
	}
}

func (p *Proxy) Command(name string, arg ...string) *exec.Cmd {
	return exec.Command(name, arg...)
}

func (p *Proxy) Compact(s []string) []string {
	return slices.Compact(s)
}

func (p *Proxy) Compare(x string, y string) int {
	return cmp.Compare(x, y)
}

func (p *Proxy) Compile(expr string) *regexp.Regexp {
	ret, err := regexp.Compile(expr)
	if err != nil {
		p.Fatal(err)
	}
	return ret
}

func (p *Proxy) Concat(s ...[]string) []string {
	return slices.Concat(s...)
}

func (p *Proxy) Contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func (p *Proxy) CreateFile(name string) *os.File {
	file, err := os.Create(name)
	if err != nil {
		p.Fatal(err)
	}
	return file
}

func (p *Proxy) Delete(s []string, i, j int) []string {
	return slices.Delete(s, i, j)
}

func (p *Proxy) Equal(a, b []byte) bool {
	return bytes.Equal(a, b)
}

func (p *Proxy) Exit(status int) {
	os.Exit(status)
}

func (p *Proxy) Fatal(v ...any) {
	log.Fatal(v...)
}

func (p *Proxy) Fatalf(format string, v ...any) {
	if len(p.expected) > 0 {
		if p.expected[0][0] == "Fatalf" && p.expected[0][1] == format && p.expected[0][2] == v[0] {
			p.expected = slices.Delete(p.expected, 0, 1)
			return
		}
	}
	log.Fatalf(format, v...)
}

func (p *Proxy) FileExists(name string) bool {
	stat, _ := os.Stat(name)
	return stat != nil
}

func (p *Proxy) GetEnvValue(key string) string {
	return os.Getenv(key)
}

func (p *Proxy) GetWorkingDirectory() string {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	return wd
}

func (p *Proxy) HasArg(id string, name string) bool {
	newVar := p.args[1:][0]
	return newVar == "-"+id || newVar == "--"+name
}

func (p *Proxy) HasArgs() bool {
	return len(p.args[1:]) > 0
}

func (p *Proxy) HasPrefix(s, prefix string) bool {
	return strings.HasPrefix(s, prefix)
}

func (p *Proxy) HasSuffix(s string, suffix string) bool {
	return strings.HasSuffix(s, suffix)
}

func (p *Proxy) Index(s, substr string) int {
	return strings.Index(s, substr)
}

func (p *Proxy) Keys(attributes map[string]*hclwrite.Attribute) iter.Seq[string] {
	return maps.Keys(attributes)
}

func (p *Proxy) MkdirAll(dir string) {
	_, err := os.Stat(dir)
	if err == nil {
		return
	}

	if !errors.Is(err, fs.ErrNotExist) {
		p.Fatal(err)
	}

	err = os.MkdirAll(dir, 0777)
	if err != nil {
		p.Fatal(err)
	}
}

func (p *Proxy) MustCompile(str string) *regexp.Regexp {
	return regexp.MustCompile(str)
}

func (p *Proxy) NewBuffer() *bytes.Buffer {
	return &bytes.Buffer{}
}

func (p *Proxy) NewDecoder(configFile *os.File) *json.Decoder {
	return json.NewDecoder(configFile)
}

func (p *Proxy) NewHclBlock(typeName string, labels []string) *hclwrite.Block {
	return hclwrite.NewBlock(typeName, labels)
}

func (p *Proxy) NewScanner(r io.Reader) *bufio.Scanner {
	return bufio.NewScanner(r)
}

func (p *Proxy) NewUuid() string {
	return uuid.New().String()
}

func (p *Proxy) NewWriter(file *os.File) bufio.Writer {
	return *bufio.NewWriter(file)
}

func (p *Proxy) Open(name string) *os.File {
	file, err := os.Open(name)
	if err != nil {
		p.Fatal(err)
	}
	return file
}

func (p *Proxy) OpenFile(name string) *os.File {
	file, err := os.OpenFile(
		name, os.O_TRUNC|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		panic(err)
	}
	return file
}

func (p *Proxy) Out(text string) {
	_, err := p.out.WriteString(text + "\n")
	if err != nil {
		p.Fatal(err)
	}
}

func (p *Proxy) Println(a ...any) {
	_, err := fmt.Println(a...)
	if err != nil {
		p.Fatal(err)
	}
}

func (p *Proxy) ReadDir(name string) []os.DirEntry {
	entries, err := os.ReadDir(name)
	if err != nil {
		p.Fatal(err)
	}
	return entries
}

func (p *Proxy) ReadFile(name string) []byte {
	bytes, err := os.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return bytes
}

func (p *Proxy) Remove(name string) {
	err := os.Remove(name)
	if err != nil {
		p.Fatal(err)
	}
}

func (p *Proxy) RemoveDir(dir string) {
	errR := os.RemoveAll(dir)
	if errR != nil {
		panic(errR)
	}
}

func (p *Proxy) ScanLines() func(data []byte, atEOF bool) (advance int, token []byte, err error) {
	return bufio.ScanLines
}

func (p *Proxy) SliceContains(array []string, value string) bool {
	return slices.Contains(array, value)
}

func (p *Proxy) Sort(x []string) {
	slices.Sort(x)
}

func (p *Proxy) SortFunc(x []*hclwrite.Block, cmp func(a *hclwrite.Block, b *hclwrite.Block) int) {
	slices.SortFunc(x, cmp)
}

func (p *Proxy) Sprintf(format string, a ...any) string {
	return fmt.Sprintf(format, a...)
}

func (p *Proxy) StringVal(s string) cty.Value {
	return cty.StringVal(">=0.0.1")
}

func (p *Proxy) Trim(s, cutset string) string {
	return strings.Trim(s, cutset)
}

func (p *Proxy) TrimLeft(s, cutset string) string {
	return strings.TrimLeft(s, cutset)
}

func (p *Proxy) TrimPrefix(s, prefix string) string {
	return strings.TrimPrefix(s, prefix)
}

func (p *Proxy) TrimSuffix(s, suffix string) string {
	return strings.TrimSuffix(s, suffix)
}

func (p *Proxy) WalkDir(root string, fn ProxyWalkDirFunc) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		return fn(path, d, err)
	})
}

func (p *Proxy) WriteFile(name string, data []byte) {
	err := os.WriteFile(name, data, 0644)
	if err != nil {
		p.Fatal(err)
	}
}

func NewProxy() *Proxy {
	return &Proxy{
		args:     os.Args,
		out:      os.Stdout,
		expected: [][]string{},
	}
}

func NewProxyMock() *Proxy {
	return &Proxy{
		args:     []string{"mock"},
		expected: [][]string{},
	}
}
