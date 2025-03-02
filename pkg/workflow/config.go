package workflow

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/rhysd/actionlint"
	"github.com/thombashi/gh-actionarmor/internal/pkg/common"
	"golang.org/x/crypto/sha3"
)

type ConfigSource string

const (
	ConfigSourceFile ConfigSource = "file"
	ConfigSourceFS   ConfigSource = "fs"
)

var ErrConfigFileNotFound = fmt.Errorf("config file not found")

type ActionArmorConfigFile struct {
	source     ConfigSource
	fileSystem fs.FS
	dirPath    string
	fileName   string
}

func NewConfigFileFromFile(path string) *ActionArmorConfigFile {
	dirPath := filepath.Dir(path)

	return &ActionArmorConfigFile{
		source:     ConfigSourceFile,
		fileSystem: os.DirFS(dirPath),
		dirPath:    ".",
		fileName:   filepath.Base(path),
	}
}

func NewConfigFileFromFS(fs fs.FS) *ActionArmorConfigFile {
	return &ActionArmorConfigFile{
		source:     ConfigSourceFS,
		fileSystem: fs,
		dirPath:    "data",
		fileName:   common.ToolName + ".yaml",
	}
}

func (c ActionArmorConfigFile) DirPath() string {
	return c.dirPath
}

func (c ActionArmorConfigFile) FileName() string {
	return c.fileName
}

func (c ActionArmorConfigFile) FilePath() string {
	return filepath.Join(c.dirPath, c.fileName)
}

// Hash returns a hash value of the instance.
// Note that the hash value is calculated based on the file path and the source of the instance and it is not a hash of the file content.
func (c ActionArmorConfigFile) Hash() string {
	hash := sha3.Sum256([]byte(string(c.source) + c.FilePath()))

	return fmt.Sprintf("%x", hash)
}

func (c ActionArmorConfigFile) ReadFile() ([]byte, error) {
	return fs.ReadFile(c.fileSystem, c.FilePath())
}

func GetConfigFile(proj *actionlint.Project) (*ActionArmorConfigFile, error) {
	var availableFileExtensions = []string{".yaml", ".yml"}

	for _, ext := range availableFileExtensions {
		fileName := common.ToolName + ext
		configFilePath := filepath.Join(proj.RootDir(), ".github", fileName)

		if fi, err := os.Stat(configFilePath); err == nil && !fi.IsDir() {
			return NewConfigFileFromFile(configFilePath), nil
		}
	}

	return nil, fmt.Errorf("%w: path=%s", ErrConfigFileNotFound, filepath.Join(proj.RootDir(), ".github"))
}
