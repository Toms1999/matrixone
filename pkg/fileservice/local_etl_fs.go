// Copyright 2022 Matrix Origin
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package fileservice

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// LocalETLFS is a FileService implementation backed by local file system and suitable for ETL operations
type LocalETLFS struct {
	name     string
	rootPath string

	sync.RWMutex
	dirFiles map[string]*os.File

	createTempDirOnce sync.Once
}

var _ FileService = new(LocalETLFS)

func NewLocalETLFS(name string, rootPath string) (*LocalETLFS, error) {
	return &LocalETLFS{
		name:     name,
		rootPath: rootPath,
		dirFiles: make(map[string]*os.File),
	}, nil
}

func (l *LocalETLFS) Name() string {
	return l.name
}

func (l *LocalETLFS) ensureTempDir() (err error) {
	l.createTempDirOnce.Do(func() {
		err = os.MkdirAll(filepath.Join(l.rootPath, ".tmp"), 0755)
	})
	return
}

func (l *LocalETLFS) Write(ctx context.Context, vector IOVector) error {
	path, err := ParsePathAtService(vector.FilePath, l.name)
	if err != nil {
		return err
	}
	nativePath := l.toNativeFilePath(path.File)

	// check existence
	_, err = os.Stat(nativePath)
	if err == nil {
		// existed
		return ErrFileExisted
	}

	return l.write(ctx, vector)
}

func (l *LocalETLFS) write(ctx context.Context, vector IOVector) error {
	path, err := ParsePathAtService(vector.FilePath, l.name)
	if err != nil {
		return err
	}
	nativePath := l.toNativeFilePath(path.File)

	// sort
	sort.Slice(vector.Entries, func(i, j int) bool {
		return vector.Entries[i].Offset < vector.Entries[j].Offset
	})

	// size
	var size int64
	if len(vector.Entries) > 0 {
		last := vector.Entries[len(vector.Entries)-1]
		size = int64(last.Offset + last.Size)
	}

	// write
	if err := l.ensureTempDir(); err != nil {
		return err
	}
	f, err := os.CreateTemp(
		filepath.Join(l.rootPath, ".tmp"),
		"*.tmp",
	)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, newIOEntriesReader(vector.Entries))
	if err != nil {
		return err
	}
	if n != size {
		return ErrSizeNotMatch
	}
	if err := f.Close(); err != nil {
		return err
	}

	// ensure parent dir
	parentDir, _ := filepath.Split(nativePath)
	err = l.ensureDir(parentDir)
	if err != nil {
		return err
	}

	// move
	if err := os.Rename(f.Name(), nativePath); err != nil {
		return err
	}

	if err := l.syncDir(parentDir); err != nil {
		return err
	}

	return nil
}

func (l *LocalETLFS) Read(ctx context.Context, vector *IOVector) error {

	if len(vector.Entries) == 0 {
		return ErrEmptyVector
	}

	path, err := ParsePathAtService(vector.FilePath, l.name)
	if err != nil {
		return err
	}
	nativePath := l.toNativeFilePath(path.File)

	_, err = os.Stat(nativePath)
	if os.IsNotExist(err) {
		return ErrFileNotFound
	}
	if err != nil {
		return err
	}

	for i, entry := range vector.Entries {
		if entry.Size == 0 {
			return ErrEmptyRange
		}

		if entry.ignore {
			continue
		}

		if entry.WriterForRead != nil {
			f, err := os.Open(nativePath)
			if os.IsNotExist(err) {
				return ErrFileNotFound
			}
			if err != nil {
				return nil
			}
			defer f.Close()
			if entry.Offset > 0 {
				if _, err := f.Seek(int64(entry.Offset), io.SeekStart); err != nil {
					return err
				}
			}
			r := (io.Reader)(f)
			if entry.Size > 0 {
				r = io.LimitReader(r, int64(entry.Size))
			}

			if entry.ToObject != nil {
				r = io.TeeReader(r, entry.WriterForRead)
				cr := &countingReader{
					R: r,
				}
				obj, size, err := entry.ToObject(cr)
				if err != nil {
					return err
				}
				vector.Entries[i].Object = obj
				vector.Entries[i].ObjectSize = size
				if cr.N != entry.Size {
					return ErrUnexpectedEOF
				}

			} else {
				n, err := io.Copy(entry.WriterForRead, r)
				if err != nil {
					return err
				}
				if n != int64(entry.Size) {
					return ErrUnexpectedEOF
				}
			}

		} else if entry.ReadCloserForRead != nil {
			f, err := os.Open(nativePath)
			if os.IsNotExist(err) {
				return ErrFileNotFound
			}
			if err != nil {
				return nil
			}
			if entry.Offset > 0 {
				if _, err := f.Seek(int64(entry.Offset), io.SeekStart); err != nil {
					return err
				}
			}
			r := (io.Reader)(f)
			if entry.Size > 0 {
				r = io.LimitReader(r, int64(entry.Size))
			}
			if entry.ToObject == nil {
				*entry.ReadCloserForRead = &readCloser{
					r:         r,
					closeFunc: f.Close,
				}
			} else {
				buf := new(bytes.Buffer)
				*entry.ReadCloserForRead = &readCloser{
					r: io.TeeReader(r, buf),
					closeFunc: func() error {
						defer f.Close()
						obj, size, err := entry.ToObject(buf)
						if err != nil {
							return err
						}
						vector.Entries[i].Object = obj
						vector.Entries[i].ObjectSize = size
						return nil
					},
				}
			}

		} else {
			f, err := os.Open(nativePath)
			if os.IsNotExist(err) {
				return ErrFileNotFound
			}
			if err != nil {
				return nil
			}
			defer f.Close()

			if entry.Offset > 0 {
				_, err = f.Seek(int64(entry.Offset), io.SeekStart)
				if err != nil {
					return err
				}
			}
			r := (io.Reader)(f)
			if entry.Size > 0 {
				r = io.LimitReader(r, int64(entry.Size))
			}

			if entry.Size < 0 {
				data, err := io.ReadAll(r)
				if err != nil {
					return err
				}
				entry.Data = data

			} else {
				if len(entry.Data) < entry.Size {
					entry.Data = make([]byte, entry.Size)
				}
				n, err := io.ReadFull(r, entry.Data)
				if err != nil {
					return err
				}
				if n != entry.Size {
					return ErrUnexpectedEOF
				}
			}

			if err := entry.setObjectFromData(); err != nil {
				return err
			}

			vector.Entries[i] = entry
		}

	}

	return nil

}

func (l *LocalETLFS) List(ctx context.Context, dirPath string) (ret []DirEntry, err error) {

	path, err := ParsePathAtService(dirPath, l.name)
	if err != nil {
		return nil, err
	}
	nativePath := l.toNativeFilePath(path.File)

	f, err := os.Open(nativePath)
	if os.IsNotExist(err) {
		err = nil
		return
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	entries, err := f.ReadDir(-1)
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		ret = append(ret, DirEntry{
			Name:  name,
			IsDir: entry.IsDir(),
			Size:  int(info.Size()),
		})
	}

	sort.Slice(ret, func(i, j int) bool {
		return ret[i].Name < ret[j].Name
	})

	if err != nil {
		return ret, err
	}

	return
}

func (l *LocalETLFS) Delete(ctx context.Context, filePath string) error {
	path, err := ParsePathAtService(filePath, l.name)
	if err != nil {
		return err
	}
	nativePath := l.toNativeFilePath(path.File)

	_, err = os.Stat(nativePath)
	if os.IsNotExist(err) {
		return ErrFileNotFound
	}
	if err != nil {
		return err
	}

	err = os.Remove(nativePath)
	if err != nil {
		return err
	}

	parentDir, _ := filepath.Split(nativePath)
	err = l.syncDir(parentDir)
	if err != nil {
		return err
	}

	return nil
}

func (l *LocalETLFS) ensureDir(nativePath string) error {
	nativePath = filepath.Clean(nativePath)
	if nativePath == "" {
		return nil
	}

	// check existence by l.dirFiles
	l.RLock()
	_, ok := l.dirFiles[nativePath]
	if ok {
		// dir existed
		l.RUnlock()
		return nil
	}
	l.RUnlock()

	// check existence by fstat
	_, err := os.Stat(nativePath)
	if err == nil {
		// existed
		return nil
	}

	// ensure parent
	parent, _ := filepath.Split(nativePath)
	if parent != nativePath {
		if err := l.ensureDir(parent); err != nil {
			return err
		}
	}

	// create
	if err := os.Mkdir(nativePath, 0755); err != nil {
		return err
	}

	// sync parent dir
	if err := l.syncDir(parent); err != nil {
		return err
	}

	return nil
}

func (l *LocalETLFS) toOSPath(filePath string) string {
	if os.PathSeparator == '/' {
		return filePath
	}
	return strings.ReplaceAll(filePath, "/", osPathSeparatorStr)
}

func (l *LocalETLFS) syncDir(nativePath string) error {
	l.Lock()
	f, ok := l.dirFiles[nativePath]
	if !ok {
		var err error
		f, err = os.Open(nativePath)
		if err != nil {
			l.Unlock()
			return err
		}
		l.dirFiles[nativePath] = f
	}
	l.Unlock()
	if err := f.Sync(); err != nil {
		return err
	}
	return nil
}

func (l *LocalETLFS) toNativeFilePath(filePath string) string {
	return filepath.Join(l.rootPath, l.toOSPath(filePath))
}

var _ ETLFileService = new(LocalETLFS)

func (l *LocalETLFS) ETLCompatible() {}

var _ MutableFileService = new(LocalETLFS)

func (l *LocalETLFS) NewMutator(filePath string) (Mutator, error) {
	path, err := ParsePathAtService(filePath, l.name)
	if err != nil {
		return nil, err
	}
	nativePath := l.toNativeFilePath(path.File)
	f, err := os.OpenFile(nativePath, os.O_RDWR, 0644)
	if os.IsNotExist(err) {
		return nil, ErrFileNotFound
	}
	return &_LocalETLFSMutator{
		osFile: f,
	}, nil
}

type _LocalETLFSMutator struct {
	osFile *os.File
}

func (l *_LocalETLFSMutator) Mutate(ctx context.Context, entries ...IOEntry) error {

	// write
	for _, entry := range entries {

		if entry.ReaderForWrite != nil {
			// seek and copy
			_, err := l.osFile.Seek(int64(entry.Offset), 0)
			if err != nil {
				return err
			}
			n, err := io.Copy(l.osFile, entry.ReaderForWrite)
			if err != nil {
				return err
			}
			if int(n) != entry.Size {
				return ErrSizeNotMatch
			}

		} else {
			// WriteAt
			n, err := l.osFile.WriteAt(entry.Data, int64(entry.Offset))
			if err != nil {
				return err
			}
			if int(n) != entry.Size {
				return ErrSizeNotMatch
			}
		}

	}

	return nil
}

func (l *_LocalETLFSMutator) Close() error {
	// sync
	if err := l.osFile.Sync(); err != nil {
		return err
	}

	// close
	if err := l.osFile.Close(); err != nil {
		return err
	}

	return nil
}
