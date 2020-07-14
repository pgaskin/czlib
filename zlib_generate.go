//+build zlib_generate

// Based on pgaskin/dictutil/marisa/libmarisa_generate.go

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
)

func main() {
	url := "https://gitlab.com/sortix/libz/-/archive/752c1630421502d6c837506d810f7918ac8cdd27/libz-752c1630421502d6c837506d810f7918ac8cdd27.tar.gz"
	version := "752c1630"

	if files, err := tarball(url); err != nil {
		fmt.Fprintf(os.Stderr, "Error: download tarball %#v: %v\n", url, err)
		os.Exit(1)
		return
	} else if err := func() error {
		if mr, err := libzlib(files, version); err != nil {
			return err
		} else if mf, err := os.OpenFile("zlib.c", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644); err != nil {
			return err
		} else if _, err := io.Copy(mf, mr); err != nil {
			mf.Close()
			return err
		} else {
			return mf.Close()
		}
	}(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: generate zlib.c: %v\n", err)
		os.Exit(1)
		return
	} else if err := func() error {
		if mr, err := hzlib(files, version); err != nil {
			return err
		} else if mf, err := os.OpenFile("zlib.h", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644); err != nil {
			return err
		} else if _, err := io.Copy(mf, mr); err != nil {
			mf.Close()
			return err
		} else {
			return mf.Close()
		}
	}(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: generate marisa.h: %v\n", err)
		os.Exit(1)
		return
	}
}
func hzlib(files map[string][]byte, version string) (io.Reader, error) {
	marisaH, err := resolve(files, []string{
		"zlib.h",
	})
	if err != nil {
		return nil, err
	}

	fmt.Printf("Generating zlib.h\n")
	return io.MultiReader(
		// A custom header.
		strings.NewReader("// AUTOMATICALLY GENERATED, DO NOT EDIT!\n"),
		strings.NewReader("// merged from sortix libz "+version+".\n"),
		// Include the header.
		bytes.NewReader(marisaH),
	), nil
}

func libzlib(files map[string][]byte, version string) (io.Reader, error) {
	dfiles := map[string][]byte{}
	for fn, b := range files {
		dfiles[fn] = b

		// fix used identifier from gzgutsneeded by inf*
		if fn == "infback.c" {
			dfiles[fn] = append([]byte("#undef COPY\n"), dfiles[fn]...)
		}

		// fix identical func redeclarations
		if fn == "deflate.c" {
			dfiles[fn] = bytes.ReplaceAll(dfiles[fn], []byte("static unsigned long saturateAddBound"), []byte("__attribute__((unused)) static unsigned long saturateAddBound_"))
		}
		if fn == "inflate.c" {
			dfiles[fn] = bytes.ReplaceAll(dfiles[fn], []byte("static void fixedtables"), []byte("__attribute__((unused)) static void fixedtables_"))
		}

		// fix identical macro redeclarations
		if fn == "inflate.c" {
			dfiles[fn] = bytes.ReplaceAll(dfiles[fn], []byte("#define PULLBYTE"), []byte("#undef PULLBYTE\n#define PULLBYTE"))
			dfiles[fn] = bytes.ReplaceAll(dfiles[fn], []byte("#define DROPBITS"), []byte("#undef DROPBITS\n#define DROPBITS"))
		}

		// we don't need endian.h to check endianness (and it isn't available on Windows)
		dfiles[fn] = bytes.ReplaceAll(dfiles[fn], []byte("#include <endian.h>"), []byte("#ifndef ZLIBGEN_ENDIAN_SHIM_H\n#define ZLIBGEN_ENDIAN_SHIM_H\n#ifndef BYTE_ORDER\n#define BYTE_ORDER __BYTE_ORDER__\n#define LITTLE_ENDIAN __ORDER_LITTLE_ENDIAN__\n#define BIG_ENDIAN __ORDER_BIG_ENDIAN__\n#endif\n#endif\n"))

		// add header guards
		if strings.HasSuffix(fn, ".h") && fn != "inffixed.h" {
			hn := "ZLIBGEN_" + strings.ToUpper(strings.TrimSuffix(fn, ".h")) + "_H"
			dfiles[fn] = append([]byte("#ifndef "+hn+"\n#define "+hn+"\n"), append(b, []byte("#endif")...)...)
		}
	}

	zlib, err := resolve(dfiles, []string{
		"zconf.h",
		"adler32.c",
		"compress.c",
		"crc32.c",
		"deflate.c",
		"gzclose.c",
		"gzlib.c",
		"gzread.c",
		"gzwrite.c",
		"infback.c",
		"inffast.c",
		"inflate.c",
		"inftrees.c",
		"trees.c",
		"uncompr.c",
		"zutil.c",
	})
	if err != nil {
		return nil, err
	}

	fmt.Printf("Generating zlib.c\n")
	return io.MultiReader(
		// A custom header.
		strings.NewReader("// AUTOMATICALLY GENERATED, DO NOT EDIT!\n"),
		strings.NewReader("// merged from sortix zlib "+version+".\n"),
		// Include the defines
		strings.NewReader("#define _GNU_SOURCE\n"),
		strings.NewReader("#define Z_INSIDE_LIBZ\n"),
		// Include the warnings from the Makefile.in CFLAGS.
		// - Note that Clang also recognizes the GCC pragmas.
		strings.NewReader("#pragma GCC diagnostic warning \"-Wall\"\n"),
		strings.NewReader("#pragma GCC diagnostic warning \"-Wextra\"\n"),
		strings.NewReader("#pragma GCC diagnostic ignored \"-Wimplicit-fallthrough\"\n"),
		// Include the libs themselves.
		bytes.NewReader(zlib),
		// Show info about the generated file.
		strings.NewReader("#line 1 \"zlib_generate.go\"\n"),
		strings.NewReader("#pragma GCC warning \"Using generated built-in sortix zlib "+version+".\"\n"),
	), nil
}

func tarball(url string) (map[string][]byte, error) {
	fmt.Printf("Downloading tarball from %s\n", url)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	zr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, err
	}

	var pfx string
	files := map[string][]byte{}

	tr := tar.NewReader(zr)
	for {
		fh, err := tr.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		if fh.Name == "pax_global_header" || fh.FileInfo().IsDir() {
			continue
		}

		if pfx == "" {
			if strings.HasPrefix(fh.Name, "./") {
				pfx = "./" + strings.Split(fh.Name, "/")[1] + "/"
			} else {
				pfx = strings.Split(fh.Name, "/")[0] + "/"
			}
		}

		if !strings.HasPrefix(fh.Name, pfx) {
			return nil, fmt.Errorf("extract file %#v: doesn't have common prefix %#v", fh.Name, pfx)
		}

		buf, err := ioutil.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("extract file %#v: %w", fh.Name, err)
		}

		fn := strings.TrimPrefix(fh.Name, pfx)
		files[fn] = buf

		fmt.Printf("  [D] %s\n", fn) // downloaded
	}

	return files, nil
}

func resolve(files map[string][]byte, filenames []string, includePath ...string) (resolvedFile []byte, err error) {
	fmt.Printf("Resolving C* source files %s (against:%s) (I = included, S = preserved because not found, R = skipped because already included)\n", filenames, includePath)

	var resolveFn func(indent string, files map[string][]byte, filename string, buf []byte, done []string, includePath []string) (resolvedFile []byte, err error)
	resolveFn = func(indent string, files map[string][]byte, filename string, buf []byte, done []string, includePath []string) (resolvedFile []byte, err error) {
		defer func() {
			if rerr := recover(); rerr != nil {
				resolvedFile, err = nil, rerr.(error)
			}
		}()

		resolvedFile = regexp.MustCompile(`(?m)^\s*#\s*include\s+["'<][^"'>]+["'>]$`).ReplaceAllFunc(buf, func(importBuf []byte) []byte {
			fn := string(regexp.MustCompile(`["'<]([^"'>]+)["'>]`).FindSubmatch(importBuf)[1])

			for _, ip := range includePath {
				ifn := path.Join(ip, fn)
				for _, dfn := range done {
					if m, _ := path.Match(dfn, ifn); m {
						fmt.Printf("%s[R] %s\n", indent, fn) // already included
						return nil
					}
				}

				ibuf, ok := files[ifn]
				if ok {
					fmt.Printf("%s[I] %s => %s\n", indent, fn, ifn) // include
					ibuf, err := resolveFn(indent+"    ", files, ifn, ibuf, append(done, ifn), append(includePath, path.Dir(ifn)))
					if err != nil {
						panic(fmt.Errorf("resolve %#v: %w", ifn, err))
					}
					return append(append([]byte{'\n', '\n'}, ibuf...), '\n', '\n')
				}
			}

			fmt.Printf("%s[S] %s\n", indent, fn) // preserve
			return importBuf
		})

		return
	}

	for _, fn := range filenames {
		if buf, ok := files[fn]; !ok {
			return nil, fmt.Errorf("file %#v: not found", fn)
		} else if buf, err := resolveFn("  ", files, fn, buf, []string{fn}, append(includePath, path.Dir(fn))); err != nil {
			return nil, fmt.Errorf("file %v: %w", fn, err)
		} else {
			resolvedFile = append(resolvedFile, buf...)
			resolvedFile = append(resolvedFile, '\n', '\n')
		}
	}

	return resolvedFile, nil
}
