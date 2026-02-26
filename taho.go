package main

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclwrite"
)

const version = "main"

// Structure for a Taho instance.
type Taho struct {
	backend   []*hclwrite.Block
	checks    []*hclwrite.Block
	complete  bool
	config    *TahoConfig
	imports   []*hclwrite.Block
	level     int
	main      []*hclwrite.Block
	num       int
	outputs   []*hclwrite.Block
	providers []*hclwrite.Block
	proxy     *Proxy
	status    int
	tempDir   string
	terraform []*hclwrite.Block
	variables []*hclwrite.Block
	version   string
}

type TahoConfig struct {
	Backend   bool     `json:"backend"`
	Ignore    []string `json:"ignore"`
	Init      bool     `json:"init"`
	Provider  bool     `json:"provider"`
	Terraform bool     `json:"terraform"`

	// In some situations, it is useful to execute Taho in a given directory but
	// actually run as if Taho is executing from within another directory.
	//
	// We achieve this behavior by setting the "workingDirectory" key for the JSON
	// content in the .taho.json file.
	WorkingDirectory string `json:"workingDirectory"`
}

func (t *Taho) AdjustBlockTypeForSorting(typeName string) string {
	if typeName == "locals" {
		return "0-" + typeName
	} else {
		return "1-" + typeName
	}
}

func (t *Taho) GetAttributeKeys(attributes map[string]*hclwrite.Attribute) []string {
	keys := []string{}
	for key := range t.proxy.Keys(attributes) {
		keys = append(keys, key)
	}
	return keys
}

func (t *Taho) GetTypeAndLabels(newBlock *hclwrite.Block) string {
	var typeAndLabels strings.Builder
	typeAndLabels.WriteString(newBlock.Type())
	labels := newBlock.Labels()
	for n := range labels {
		typeAndLabels.WriteString(t.proxy.Sprintf(".%s", labels[n]))
	}
	return typeAndLabels.String()
}

func (t *Taho) HandleArgs() {
	if t.proxy.HasArgs() {
		if t.HandleTestArg() {
			return
		}

		if t.HandleInitArg() {
			return
		}

		if t.HandleHelpArg() {
			return
		}

		if t.HandleRecursiveArg() {
			return
		}

		if t.HandleVersionArg() {
			return
		}

		t.proxy.Fatalf("Unable to handle argumet \"%s\"", t.proxy.args[1:][0])
	}
}

func (t *Taho) HandleHelpArg() bool {
	if !(t.proxy.HasArg("h", "help")) {
		return false
	}

	t.proxy.Println(
		"Usage: taho [options]\n" +
			"\n" +
			"Options: are as follows\n" +
			"\n" +
			"-h, --help\n" +
			"-i, --init\n" +
			"-r, --recursive\n" +
			"-v, --version")
	t.complete = true
	return true
}

func (t *Taho) HandleInitArg() bool {
	if !(t.proxy.HasArg("i", "init")) {
		return false
	}

	t.config.Init = true
	return true
}

func (t *Taho) HandleRecursiveArg() bool {
	if !(t.proxy.HasArg("r", "recursive")) {
		return false
	}

	wd := t.proxy.GetWorkingDirectory() + "/"

	list := []string{}
	err := t.proxy.WalkDir(".", func(path string, d ProxyDirEntry, err error) error {
		if err != nil {
			panic(err)
		}
		path = wd + path
		if !t.proxy.Contains(path, "/.") &&
			!t.proxy.Contains(path, "/_") &&
			!t.proxy.Contains(path, "/taho/") {

			if d.IsDir() {
				list = append(list, path)
			}

		}
		return nil
	})

	if err != nil {
		panic(err)
	}

	for n := range list {
		dir := list[n]
		if !t.proxy.SliceContains(t.config.Ignore, dir) {
			t.proxy.Chdir(dir)

			if t.config.Init {
				tfCmd := "init"
				cmd := t.proxy.Command("tofu", tfCmd)
				cmd.Stdout = t.proxy.NewBuffer()
				cmd.Stderr = t.proxy.NewBuffer()
				cmdErr := cmd.Run()

				if cmdErr != nil {
					cmd = t.proxy.Command("terraform", tfCmd)
					cmd.Stdout = t.proxy.NewBuffer()
					cmd.Stderr = t.proxy.NewBuffer()
					cmdErr = cmd.Run()

					if cmdErr != nil {
						panic(cmdErr)
					}
				}
			}

			if t.IsTestable() {
				if n > 0 {
					t.Out("")
				}
				t.Out(dir)
				if dir == "." {
					dir = ""
				}
			}

			t2 := Taho{
				version: version,
				proxy:   t.proxy,
				config:  t.config,
			}

			t2.RunAsNeeded()
		}
	}

	return true
}

func (t *Taho) HandleTestArg() bool {
	if !(t.proxy.HasArg("t", "test")) {
		return false
	}
	return true
}

func (t *Taho) HandleVersionArg() bool {
	if !(t.proxy.HasArg("v", "version")) {
		return false
	}

	t.proxy.Println(version)
	t.complete = true
	return true
}

// Check if an array of lines fro a block is a multiline attribute.
//
// If the array of lines has a non-comment line size that equals 3 it is a
// a single line attribute.
func (t *Taho) IfMultiline(lines []string) bool {
	size := 0
	for n := range lines {
		if !t.proxy.HasPrefix(lines[n], "#") {
			size++
		}
	}
	return size != 3
}

func (t *Taho) IsTestable() bool {
	entries := t.proxy.ReadDir("./")

	for _, entry := range entries {
		if entry.Type().IsRegular() {
			name := entry.Name()
			p := t.proxy
			if p.HasSuffix(name, ".tf") || p.HasSuffix(name, ".hcl") ||
				p.HasSuffix(name, ".tfvars") {
				return true
			}
		}
	}
	return false
}

func (t *Taho) LoadConfig() {
	config := TahoConfig{}
	config.Backend = true
	config.Terraform = true
	config.Provider = true
	t.config = &config
	t.LoadConfigFile(os.Getenv("HOME")+"/.taho.json", config)
	t.LoadConfigFile(".taho.json", config)

	if t.config.WorkingDirectory != "" {
		t.proxy.Chdir(t.config.WorkingDirectory)
	}
}

func (t *Taho) LoadConfigFile(filename string, config TahoConfig) {
	configFile, err := os.Open(filename)
	if err != nil {
		return
	}
	defer configFile.Close()
	jsonParser := json.NewDecoder(configFile)
	jsonParser.Decode(&config)
	t.config = &config
}

func (t *Taho) Num() int {
	t.num++
	return t.num
}

func (t *Taho) Out(text string) {
	t.proxy.Out(text)
}

func (t *Taho) ParseConfig(filename string) ([]byte, *hclwrite.File) {
	fileBytes := t.proxy.ReadFile(filename)
	pos := hcl.InitialPos
	file, diag := hclwrite.ParseConfig(fileBytes, filename, pos)

	if diag.HasErrors() {
		return fileBytes, nil
	}

	return fileBytes, file
}

func (t *Taho) ProcessFile(filename string, hasTF bool, specialNames map[string]bool) bool {
	isOverride := t.proxy.HasPrefix(filename, "_")
	isTF := t.proxy.HasSuffix(filename, ".tf")
	isHCL := t.proxy.HasSuffix(filename, ".hcl") && filename != ".terraform.lock.hcl"

	if !isOverride {
		if t.proxy.HasSuffix(filename, ".tfvars") {
			t.RewriteTfVars(filename)

		} else if isTF || isHCL {
			hasTF = hasTF || isTF
			_, isSpecial := specialNames[filename]

			fileBytes, file := t.ParseConfig(filename)

			blocks := []*hclwrite.Block{}
			for _, block := range file.Body().Blocks() {
				if isHCL {
					blocks = append(blocks, block)
				} else {
					if block.Type() == "check" {
						t.checks = append(t.checks, block)
					} else if block.Type() == "import" {
						t.imports = append(t.imports, block)
					} else if block.Type() == "terraform" && t.config.Terraform {
						s3Label := []string{"s3"}
						backendBlock := block.Body().FirstMatchingBlock("backend", s3Label)
						if backendBlock != nil && t.config.Backend {
							tfBlockLabels := []string{}
							tfBackendBlock := t.proxy.NewHclBlock("terraform", tfBlockLabels)
							tfBackendBlock.Body().AppendBlock(backendBlock)
							t.backend = append(t.backend, tfBackendBlock)
							block.Body().RemoveBlock(backendBlock)
						}
						if filename != "backend.tf" {
							t.terraform = append(t.terraform, block)
						}
					} else if block.Type() == "provider" && t.config.Provider {
						t.providers = append(t.providers, block)
					} else if block.Type() == "variable" {
						t.variables = append(t.variables, block)
					} else if block.Type() == "output" {
						t.outputs = append(t.outputs, block)
					} else if isSpecial {
						t.main = append(t.main, block)
					} else {
						blocks = append(blocks, block)
					}
				}
			}

			var newFile *hclwrite.File
			if isTF {
				newFile = hclwrite.NewFile()
			} else {
				_, file := t.ParseConfig(filename)
				newFile = file
				fileBlocks := file.Body().Blocks()

				for _, block := range fileBlocks {
					newFile.Body().RemoveBlock(block)
				}

				newBytes := newFile.Bytes()
				t.proxy.WriteFile(filename, newBytes)
				lines1, _, _ := t.ReadLines(filename, "")
				newLines := t.proxy.Concat([]string{"terragrunt {"}, lines1, []string{"}"})
				temp1 := t.proxy.Sprintf("%s/%d.hcl", t.tempDir, t.Num())
				t.WriteLines(temp1, newLines)
				_, file = t.ParseConfig(temp1)
				block := file.Body().Blocks()[0]
				block = t.RewriteBlock(block, false)
				lines2 := t.ToLines(block)
				lines2 = t.proxy.Delete(lines2, 0, 1)
				lines2 = t.proxy.Delete(lines2, len(lines2)-1, len(lines2))
				lines3 := []string{}

				for _, line := range lines2 {
					line = t.proxy.TrimPrefix(line, "  ")
					lines3 = append(lines3, line)
				}

				if len(lines3) > 0 {
					lines3 = append(lines3, "")
				}

				temp3 := t.proxy.Sprintf("%s/%d.hcl", t.tempDir, t.Num())
				t.WriteLines(temp3, lines3)
				_, file = t.ParseConfig(temp3)
				newFile = file
			}

			blocks = t.RewriteBlocks(blocks, true)
			keys := t.GetAttributeKeys(newFile.Body().Attributes())
			keysLen := len(keys)
			blocksLen := len(blocks)
			newFileBody := newFile.Body()

			for i, block := range blocks {
				newFileBody.AppendBlock(block)

				if i < blocksLen-1 {
					newFileBody.AppendNewline()
				}
			}

			newBytes := newFile.Bytes()

			if !isSpecial {

				if !t.proxy.Equal(fileBytes, newBytes) {
					text := t.proxy.Sprintf("File mismatch: \"%s\"", filename)
					t.Out(text)
					t.SetErrExit()
				}

				if blocksLen == 0 && keysLen == 0 {
					t.proxy.Remove(filename)
				} else {
					t.proxy.WriteFile(filename, newBytes)
				}
			}
		}
	}

	return hasTF
}

func (t *Taho) ReadBlock(filename string) *hclwrite.Block {
	_, file := t.ParseConfig(filename)
	if file == nil {
		return nil
	}
	return file.Body().Blocks()[0]
}

func (t *Taho) ReadBlockX(temp string) *hclwrite.Block {
	tempBlock := t.ReadBlock(temp)
	return tempBlock
}

func (t *Taho) RemoveTrailingComments(block *hclwrite.Block, fixOtherComments bool,
	key string) *hclwrite.Block {

	temp := t.WriteBlock(block)
	open := t.proxy.Open(temp)
	s := t.proxy.NewScanner(open)
	s.Split(t.proxy.ScanLines())
	lines := []string{}
	mode := 0

	// Remove blank lines because they cause problems with parsing
	blockPrefix := block.Type() + " "
	keyPrefix := "  " + key + " = "
	inspan := false
	inblock := false
	for s.Scan() {
		line := s.Text()
		if fixOtherComments {
			if t.proxy.HasPrefix(line, blockPrefix) {
				fixOtherComments = key != ""
				inblock = true
			} else if t.proxy.HasPrefix(line, keyPrefix) {
				fixOtherComments = false
			} else {
				trim := t.proxy.TrimLeft(line, " ")

				if t.proxy.HasPrefix(trim, "//") {
					line = "# " + t.proxy.TrimPrefix(t.proxy.TrimPrefix(trim, "//"), " ")
				} else if t.proxy.HasPrefix(trim, "/*") {
					line = t.proxy.TrimPrefix(trim, "/*")
					if line != "" {
						line = "# " + line
						if inblock {
							line = "  " + line
						}
					}
					inspan = true
				}

				if inspan {
					pos := t.proxy.Index(line, "*/")
					if pos >= 0 {
						line = line[pos+2:]
						inspan = false
					} else {
						line = t.proxy.TrimLeft(line, " ")
						line = t.proxy.TrimPrefix(line, "* ")
						if line != "" {
							line = "# " + line
							if inblock {
								line = "  " + line
							}
						}
					}
				}
			}
		}
		if line != "" || mode > 1 {
			lines = append(lines, line)
			mode++
		}
	}
	lenLines := len(lines)
	newLines := []string{}
	strippingComments := true
	for line := range lines {
		text := lines[lenLines-1-line]
		if strippingComments {
			trimmedText := t.proxy.TrimLeft(text, " ")
			if t.proxy.HasPrefix(trimmedText, "#") {
				text = ""
			} else {
				if trimmedText != "}" {
					strippingComments = false
				}
			}
		}
		if text != "" {
			newLines = append(newLines, text)
		}
	}

	lines = []string{}
	lenNewLines := len(newLines)
	for line := range newLines {
		text := newLines[lenNewLines-1-line]
		lines = append(lines, text)
	}

	temp = t.proxy.Sprintf("%s/%d.hcl", t.tempDir, t.Num())
	t.WriteLines(temp, lines)
	block = t.ReadBlock(temp)
	return block
}

func (t *Taho) RewriteBlock(block *hclwrite.Block,
	metaMode bool) *hclwrite.Block {

	temp1 := t.WriteBlock(block)
	_, _, hasHeredoc := t.ReadLines(temp1, "")
	if hasHeredoc {
		return block
	}

	detatchedNestedBlocks := make([]*hclwrite.Block, 0)
	bodyBlocks := block.Body().Blocks()
	lenBodyBlocks := len(bodyBlocks)
	blockTypes := []string{}
	for b := range bodyBlocks {
		newBlock := hclwrite.NewBlock(block.Type(), block.Labels())
		movedBlock := bodyBlocks[lenBodyBlocks-1-b]
		newBlock.Body().AppendBlock(movedBlock)
		block.Body().RemoveBlock(movedBlock)
		typeAndLabels := t.GetTypeAndLabels(movedBlock)
		blockTypes = append(blockTypes, typeAndLabels)
		detatchedNestedBlocks = append(detatchedNestedBlocks, newBlock)
	}

	t.proxy.Sort(blockTypes)
	blockTypes = t.proxy.Compact(blockTypes)

	if metaMode {
		metaBlocks := map[string]bool{
			"connection":  true,
			"lifecycle":   true,
			"provisioner": true,
		}
		lastBlockTypes := len(blockTypes) - 1
		for b := range blockTypes {
			if b < lastBlockTypes {
				if metaBlocks[blockTypes[b]] {
					blockTypes[b] = blockTypes[b+1]
					blockTypes[b+1] = "lifecycle"
				}
			}
		}
	}

	block = t.SortAttributes(block, metaMode)

	keys := t.GetAttributeKeys(block.Body().Attributes())
	newLineNeeded := len(keys) > 0

	lenNestedBlocks := len(detatchedNestedBlocks)
	blockBody := block.Body()

	for n := range blockTypes {
		orderedBlockType := blockTypes[n]
		for b := range detatchedNestedBlocks {
			detachedBlock := detatchedNestedBlocks[lenNestedBlocks-1-b]
			tempBlock := t.RemoveTrailingComments(detachedBlock, false, "")
			tempBlocks := tempBlock.Body().Blocks()
			for tbb := range tempBlocks {
				nestedBlock := tempBlocks[tbb]
				typeAndLabels := t.GetTypeAndLabels(nestedBlock)
				if typeAndLabels == orderedBlockType {
					if newLineNeeded {
						blockBody.AppendNewline()
					}
					nestedBlock = t.RewriteBlock(nestedBlock, metaMode)
					blockBody.AppendBlock(nestedBlock)
					newLineNeeded = true
				}
			}
		}
	}

	return block
}

func (t *Taho) RewriteBlocks(blocks []*hclwrite.Block,
	metaMode bool) []*hclwrite.Block {

	blockCmp := func(a *hclwrite.Block, b *hclwrite.Block) int {
		aTypeName := t.AdjustBlockTypeForSorting(a.Type())
		bTypeName := t.AdjustBlockTypeForSorting(b.Type())
		typeOrder := t.proxy.Compare(aTypeName, bTypeName)
		if typeOrder != 0 {
			return typeOrder
		}

		aLabel0 := ""
		aLabels := a.Labels()
		if len(aLabels) > 0 {
			aLabel0 = aLabels[0]
		}

		bLabels := b.Labels()
		bLabel0 := ""
		if len(bLabels) > 0 {
			bLabel0 = bLabels[0]
		}

		label0Order := t.proxy.Compare(aLabel0, bLabel0)
		if label0Order != 0 {
			return label0Order
		}

		aLabel1 := ""
		bLabel1 := ""
		if len(aLabels) > 1 {
			aLabel1 = aLabels[1]
		}

		if len(bLabels) > 1 {
			bLabel1 = bLabels[1]
		}
		return t.proxy.Compare(aLabel1, bLabel1)
	}

	t.proxy.SortFunc(blocks, blockCmp)

	newBlocks := make([]*hclwrite.Block, 0)
	for b := range blocks {
		block := blocks[b]
		block = t.RewriteBlock(block, metaMode)
		newBlocks = append(newBlocks, block)
	}

	return newBlocks
}

func (t *Taho) RewriteTfVars(filename string) {
	lines, _, _ := t.ReadLines(filename, "")

	mapLines := []string{"map {"}
	mapLines = append(mapLines, lines...)
	mapLines = append(mapLines, "}")

	temp1 := t.proxy.Sprintf("%s/%d.hcl", t.tempDir, t.Num())
	t.WriteLines(temp1, mapLines)
	mapBlock := t.ReadBlockX(temp1)

	if mapBlock == nil {
		return
	}

	mapBlock = t.RewriteBlock(mapBlock, false)

	temp2 := t.WriteBlock(mapBlock)
	mapLines, _, _ = t.ReadLines(temp2, "")
	newLines := mapLines[1 : len(mapLines)-1]

	temp3 := t.proxy.Sprintf("%s/%d.tf", t.tempDir, t.Num())
	t.WriteLines(temp3, newLines)

	tfCmd := "fmt"
	cmd := t.proxy.Command("terraform", tfCmd, temp3)
	cmd.Stdout = t.proxy.NewBuffer()
	cmd.Stderr = t.proxy.NewBuffer()
	cmdErr := cmd.Run()

	if cmdErr != nil {
		panic(cmdErr)
	}

	fmtLines, _, _ := t.ReadLines(temp3, "")

	mismatch := false
	if len(fmtLines) == len(lines) {

		for n := range fmtLines {
			if fmtLines[n] != lines[n] {
				mismatch = true
				break
			}
		}

	} else {
		mismatch = true
	}

	if mismatch {
		text := t.proxy.Sprintf("File mismatch: \"%s\"", filename)
		t.Out(text)
		t.WriteLines(filename, fmtLines)
		t.SetErrExit()
	}
}

func (t *Taho) Run() {
	t.HandleArgs()
	t.RunAsNeeded()
	t.RunTerraformFmt()
	t.proxy.Exit(t.status)
}

// Run Taho as needed.
//
// Do to the nature of changes this program makes there are cases where a change
// that has occured results in a situation where running the program again will
// issues that did not exist the first time the program ran.  As a result, we
// run the program times to reach a state that does not need needs subsequent
// changes.
func (t *Taho) RunAsNeeded() {
	t.RunIfNeeded()

	done := t.status == 0

	for !done {
		last := t

		t := Taho{
			config:  last.config,
			level:   last.level + 1,
			proxy:   last.proxy,
			version: version,
		}

		t.RunIfNeeded()
		done = t.status == 0

		if t.status > 0 {
			t.status += last.status
		}

		if t.status > 3 {
			t.proxy.Fatalf("Unable to complete run; failure with %d status", t.status)
		}
	}
}

// Run the program if needed.
//
// Some cases exist where we don't actually need to run the main program logic.
// An example is invoking the CLI to request the version using the `-v` option.
func (t *Taho) RunIfNeeded() {
	if t.complete {
		return
	}

	home := t.proxy.GetEnvValue("HOME")
	uuid := t.proxy.NewUuid()
	t.tempDir = t.proxy.Sprintf("%s/.taho/%s/%d-%d", home, uuid, t.level, t.Num())
	t.proxy.MkdirAll(t.tempDir)

	// A number of filenames exist have special style rules.
	specialNames := map[string]bool{
		"backend.tf":   true,
		"checks.tf":    true,
		"imports.tf":   true,
		"main.tf":      true,
		"outputs.tf":   true,
		"providers.tf": true,
		"terraform.tf": true,
		"variables.tf": true,
	}

	// Process files unless a "tofu" version of the filename also exists.
	entries := t.proxy.ReadDir("./")
	hasTF := false
	for _, entry := range entries {
		entryName := entry.Name()
		if !t.TofuExists(entryName) {
			hasTF = t.ProcessFile(entryName, hasTF, specialNames)
		}
	}

	if hasTF {
		if len(t.terraform) == 0 && t.config.Terraform {
			blockLabels := []string{}
			newBlock := t.proxy.NewHclBlock("terraform", blockLabels)
			newValue := t.proxy.StringVal(">=0.0.1")
			newBlock.Body().SetAttributeValue("required_version", newValue)
			t.terraform = append(t.terraform, newBlock)
		}

		// We write these tf files if we have content for them
		t.WriteTfFile(false, "backend.tf", t.backend)
		t.WriteTfFile(false, "checks.tf", t.checks)
		t.WriteTfFile(false, "import.tf", t.imports)
		t.WriteTfFile(false, "outputs.tf", t.outputs)
		t.WriteTfFile(false, "variables.tf", t.variables)

		// We write out main.tf even if it is empty.
		t.WriteTfFile(true, "main.tf", t.main)

		if t.config.Provider {
			t.WriteTfFile(false, "providers.tf", t.providers)
		}

		if t.config.Terraform {
			t.WriteTfFile(true, "terraform.tf", t.terraform)
		}
	}

	if !t.proxy.HasSuffix(t.version, "-0") {
		t.proxy.RemoveDir(t.tempDir)
	}
}

func (t *Taho) RunTerraformFmt() {
	tfCmd := "fmt"
	cmd := t.proxy.Command("terraform", tfCmd)
	cmd.Stdout = t.proxy.NewBuffer()
	cmd.Stderr = t.proxy.NewBuffer()
	cmdErr := cmd.Run()
	if cmdErr != nil {
		panic(cmdErr)
	}
}

func (t *Taho) SetErrExit() {
	if t.status == 0 {
		t.status = 1
	}
}

func (t *Taho) SortAttributeKeys(keys []string,
	metaArguments map[string]bool) []string {

	t.proxy.Sort(keys)
	metaKeys := []string{}
	nonMetaKeys := []string{}
	for k := range keys {
		key := keys[k]
		_, metaArgument := metaArguments[key]
		if metaArgument {
			metaKeys = append(metaKeys, key)
		} else {
			nonMetaKeys = append(nonMetaKeys, key)
		}
	}

	keys = append(metaKeys, nonMetaKeys...)
	return keys
}

func (t *Taho) SortAttributes(block *hclwrite.Block,
	metaArgMode bool) *hclwrite.Block {

	metaArguments := map[string]bool{
		"count":      true,
		"depends_on": true,
		"for_each":   true,
		"provider":   true,
		"providers":  true,
	}

	if block.Type() == "locals" {
		metaArgMode = false
	}

	if block.Type() == "module" {
		metaArguments["version"] = true
		metaArguments["source"] = true
	}

	if !metaArgMode {
		metaArguments = map[string]bool{}
	}

	keys := t.GetAttributeKeys(block.Body().Attributes())
	keys = t.SortAttributeKeys(keys, metaArguments)

	block = t.RemoveTrailingComments(block, len(keys) == 0, "")
	temp := t.WriteBlock(block)
	lines, start, _ := t.ReadLines(temp, "#")

	multiLineKeys := []string{}
	multiLineMetaKeys := []string{}
	singleLineKeys := []string{}
	singleLineMetaKeys := []string{}

	for k1 := range keys {
		key := keys[k1]

		tempBlock := t.ReadBlockX(temp)
		for k2 := range keys {
			if keys[k1] != keys[k2] {
				tempBlock.Body().RemoveAttribute(keys[k2])
			}
		}

		tempBlock = t.RemoveTrailingComments(tempBlock, true, key)

		lines2, _, _ := t.ReadLines(t.WriteBlock(tempBlock), "")
		isMultiLine := t.IfMultiline(lines2)
		_, metaKey := metaArguments[key]
		if isMultiLine {
			if metaKey {
				multiLineMetaKeys = append(multiLineMetaKeys, key)
			} else {
				multiLineKeys = append(multiLineKeys, key)
			}
		} else {
			if metaKey {
				singleLineMetaKeys = append(singleLineMetaKeys, key)
			} else {
				singleLineKeys = append(singleLineKeys, key)
			}
		}
	}

	keys = append(singleLineMetaKeys, multiLineMetaKeys...)
	keys = append(keys, singleLineKeys...)
	keys = append(keys, multiLineKeys...)

	hasProcessedOneKey := false
	metaArgumentSection := false
	if len(keys) > 0 {
		metaArgumentSection = metaArguments[keys[0]]
	}
	for k1 := range keys {
		key := keys[k1]
		_, metaArgument := metaArguments[key]

		if !metaArgument {
			if metaArgumentSection {
				lines = append(lines, "")
				metaArgumentSection = false
			}
		}

		tempBlock := t.ReadBlock(temp)
		for k2 := range keys {
			if keys[k2] != key {
				tempBlock.Body().RemoveAttribute(keys[k2])
			}
		}

		tempBlock = t.RemoveTrailingComments(tempBlock, true, keys[0])

		temp := t.WriteBlock(tempBlock)
		lines2, _, _ := t.ReadLines(temp, "")
		isMultiLine := t.IfMultiline(lines2)

		if isMultiLine {
			lines4 := []string{}
			for n := range lines2 {
				line := lines2[n]

				if t.proxy.HasPrefix(t.proxy.TrimLeft(line, " "),
					t.proxy.Sprintf("%s = ", key)) {

					regex := t.proxy.Compile(`^[A-Za-z0-9_-]*$`)
					if t.proxy.HasSuffix(line, "= {") {
						lines3 := lines2[n+1 : len(lines2)-1]
						for k := range lines3 {
							line := lines3[k]
							line = t.proxy.TrimSuffix(line, ",")
							if t.proxy.HasPrefix(line, "    \"") {
								eidx := t.proxy.Index(line, "=")
								key = t.proxy.Trim(line[0:eidx-1], " ")
								key = t.proxy.TrimPrefix(key, "\"")
								key = t.proxy.TrimSuffix(key, "\"")
								matched := regex.MatchString(key)
								if matched {
									line = "    " + key + " " + line[eidx:]
								}
								lines3[k] = line
							}
						}
						body := []string{"map {"}
						body = append(body, lines3...)
						body = body[:len(body)-1]
						body = append(body, "}")
						temp3 := t.proxy.Sprintf("%s/%d.hcl", t.tempDir, t.Num())
						t.WriteLines(temp3, body)
						mapBlock := t.ReadBlock(temp3)
						if mapBlock != nil {
							mapBlock = t.RewriteBlock(mapBlock, false)
							body, _, _ = t.ReadLines(t.WriteBlock(mapBlock), "")
							n1 := 0
							lines3 = []string{}
							for {
								lines3 = append(lines3, lines2[n1])
								n1++
								if n1 > n {
									break
								}
							}
							lines3 = append(lines3, body[1:]...)
							lines3 = append(lines3, "}")
							temp5 := t.proxy.Sprintf("%s/%d.hcl", t.tempDir, t.Num())
							t.WriteLines(temp5, lines3)
							mapBlock = t.ReadBlock(temp5)
							if mapBlock != nil {
								lines4, _, _ = t.ReadLines(t.WriteBlock(mapBlock), "")
							}
						}
					}
				}
			}

			if len(lines4) > 0 {
				lines2 = lines4
			}
		}

		if hasProcessedOneKey {
			if isMultiLine {
				lines = append(lines, "")
			}
		}

		for n := range lines2 {
			if n > start && n < len(lines2)-1 {
				lines = append(lines, lines2[n])
			}
		}

		if !metaArgument {
			hasProcessedOneKey = true
		}
	}

	if !t.proxy.HasSuffix(lines[start], "{}") {
		lines = append(lines, "}")
	}

	temp3 := t.proxy.Sprintf("%s/%d.hcl", t.tempDir, t.Num())
	t.WriteLines(temp3, lines)
	rewrittenBlock := t.ReadBlock(temp3)
	if rewrittenBlock != nil {
		block = rewrittenBlock
	}
	return block
}

func (t *Taho) TofuExists(filename string) bool {
	re := t.proxy.MustCompile(`\.tf$`)
	tofuName := re.ReplaceAllString(filename, "") + ".tofu"
	return t.proxy.FileExists(tofuName)
}

func (t *Taho) ToLines(block *hclwrite.Block) []string {
	lines, _, _ := t.ReadLines(t.WriteBlock(block), "")
	return lines
}

func (t *Taho) WriteBlock(block *hclwrite.Block) string {
	temp := t.proxy.Sprintf("%s/%d.hcl", t.tempDir, t.Num())
	tempName := block.Type()
	tempFile := hclwrite.NewFile()
	tempFile.Body().AppendBlock(block)
	tempBytes := tempFile.Bytes()
	labels := block.Labels()
	for i := range labels {
		tempName += t.proxy.Sprintf("-%s", labels[i])
	}
	t.proxy.WriteFile(temp, tempBytes)
	return temp
}

func (t *Taho) WriteLines(filename string, lines []string) {
	openFile := t.proxy.OpenFile(filename)
	writer := t.proxy.NewWriter(openFile)
	for line := range lines {
		lineText := lines[line]
		writer.WriteString(lineText)
		writer.WriteString("\n")
	}
	writer.Flush()
}

func (t *Taho) WriteTfFile(always bool, filename string, blocks []*hclwrite.Block) {
	if t.TofuExists(filename) {
		return
	}

	if !always {
		if len(blocks) == 0 {
			if t.proxy.FileExists(filename) {
				t.proxy.Remove(filename)
			}
			return
		}
	}

	fileBytes := []byte{}
	if t.proxy.FileExists(filename) {
		fileBytes = t.proxy.ReadFile(filename)
	}

	if len(blocks) == 0 {
		file := t.proxy.CreateFile(filename)
		defer file.Close()
	} else {
		file := hclwrite.NewFile()

		blocks = t.RewriteBlocks(blocks, true)

		len := len(blocks)
		body := file.Body()
		for i, block := range blocks {
			body.AppendBlock(block)
			if i < len-1 {
				body.AppendNewline()
			}
		}

		newBytes := file.Bytes()

		if !t.proxy.Equal(fileBytes, newBytes) {
			text := t.proxy.Sprintf("Managed file mismatch: \"%s\"", filename)
			t.Out(text)
			t.SetErrExit()
		}

		t.proxy.WriteFile(filename, newBytes)
	}
}

func (t Taho) ReadLines(filename string, stopPrefix string) ([]string, int, bool) {
	lines := []string{}
	open := t.proxy.Open(filename)
	s := t.proxy.NewScanner(open)
	s.Split(t.proxy.ScanLines())
	hasHeredoc := false
	for s.Scan() {
		text := s.Text()
		hasHeredoc = hasHeredoc || t.proxy.Contains(text, "<<")
		lines = append(lines, text)
		if !t.proxy.HasPrefix(text, stopPrefix) {
			break
		}
	}
	start := len(lines) - 1
	return lines, start, hasHeredoc
}

func NewTaho() *Taho {
	t := &Taho{
		proxy:   NewProxy(),
		version: version,
	}
	t.LoadConfig()
	return t
}

func NewTahoWithMockProxy() *Taho {
	t := &Taho{
		proxy:   NewProxyMock(),
		version: version,
	}
	t.LoadConfig()
	return t
}
