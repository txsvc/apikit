package api

import (
	"context"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ExtractHeaders extracts the relevant HTTP header stuff only
func ExtractHeaders(r *http.Request) RelevantHeaders {
	h := RelevantHeaders{
		Range:           r.Header.Get("Range"),
		UserAgent:       r.Header.Get("User-Agent"),
		Forwarded:       r.Header.Get("Forwarded"),
		XForwardedFor:   r.Header.Get("X-Forwarded-For"),
		XForwwardedHost: r.Header.Get("X-Forwarded-Host"),
		Referer:         r.Header.Get("Referer"),
	}
	return h
}

// ParseRange extracts a byte range if specified. For specs see
// https://developer.mozilla.org/en-US/docs/Web/HTTP/Range_requests
func ParseRange(r string) (int64, int64) {
	if r == "" {
		return 0, -1 // no range requested
	}
	parts := strings.Split(r, "=")
	if len(parts) != 2 {
		return 0, -1 // no range requested
	}
	// we simply assume that parts[0] == "bytes"
	ra := strings.Split(parts[1], "-")
	if len(ra) != 2 { // again a simplification, multiple ranges or overlapping ranges are not supported
		return 0, -1
	}

	start, err := strconv.ParseInt(ra[0], 10, 64)
	if err != nil {
		return 0, -1
	}
	end, err := strconv.ParseInt(ra[1], 10, 64)
	if err != nil {
		return 0, -1
	}

	return start, end - start
}

func Duration(d time.Duration, dicimal int) time.Duration {
	shift := int(math.Pow10(dicimal))

	units := []time.Duration{time.Second, time.Millisecond, time.Microsecond, time.Nanosecond}
	for _, u := range units {
		if d > u {
			div := u / time.Duration(shift)
			if div == 0 {
				break
			}
			d = d / div * div
			break
		}
	}
	return d
}

// HandleFileUpload receives files from stores them locally
func HandleFileUpload(ctx context.Context, req *http.Request, location, formName string) (string, error) {
	var path string

	// FIXME: treat location as a 'bucket' in preparation of switching to a generic storage API

	mr, err := req.MultipartReader()
	if err != nil {
		return "", err
	}

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		if part.FormName() == formName {
			path = filepath.Join(location, part.FileName())

			if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil { // make sure sub-folders exist
				return "", err
			}
			out, err := os.Create(path)
			if err != nil {
				return "", err
			}
			defer func() {
				_ = out.Close() // Ignore error on close
			}()

			if _, err := io.Copy(out, part); err != nil {
				return "", err
			}
		}
	}

	return path, nil // FIXME: do we need the path ?
}
