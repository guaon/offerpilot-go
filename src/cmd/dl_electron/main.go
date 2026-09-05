package main

import (
	"archive/zip"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func main() {
	// 尝试 npm 淘宝镜像
	urls := []string{
		"https://registry.npmmirror.com/-/binary/electron/v33.4.0/electron-v33.4.0-win32-x64.zip",
		"https://cdn.npm.taobao.org/dist/electron/v33.4.0/electron-v33.4.0-win32-x64.zip",
	}

	client := &http.Client{
		Timeout: 300 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	for _, url := range urls {
		fmt.Printf("Trying %s...\n", url)
		resp, err := client.Get(url)
		if err != nil {
			fmt.Printf("  Error: %v\n", err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			fmt.Printf("  HTTP %d\n", resp.StatusCode)
			continue
		}

		outPath := filepath.Join(os.Getenv("APPDATA"), "DSH Desktop", "electron.zip")
		extractDir := filepath.Join(os.Getenv("APPDATA"), "DSH Desktop", "electron")

		f, _ := os.Create(outPath)
		written, _ := io.Copy(f, resp.Body)
		f.Close()
		fmt.Printf("  Downloaded %d bytes\n", written)

		fmt.Printf("  Extracting...\n")
		os.MkdirAll(extractDir, 0755)
		reader, _ := zip.OpenReader(outPath)
		for _, file := range reader.File {
			path := filepath.Join(extractDir, file.Name)
			if file.FileInfo().IsDir() {
				os.MkdirAll(path, 0755)
				continue
			}
			os.MkdirAll(filepath.Dir(path), 0755)
			src, _ := file.Open()
			dst, _ := os.Create(path)
			io.Copy(dst, src)
			src.Close()
			dst.Close()
		}
		reader.Close()
		fmt.Printf("  Done! %s\n", filepath.Join(extractDir, "electron.exe"))
		return
	}
	fmt.Println("All mirrors failed")
}