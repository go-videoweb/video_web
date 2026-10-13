package scanner

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"yssim-go/tests/tools/api_coverage/coverage"
)

// ScanExecutedMarks 从一个或多个 go test -json 文件中扫描 API_MARK。
// 支持紧凑 JSONL，也支持 PowerShell/TeamCity 可能保存出的连续多行 JSON 对象流。
func ScanExecutedMarks(jsonPaths []string) ([]coverage.Mark, error) {
	var marks []coverage.Mark
	for _, jsonPath := range jsonPaths {
		events, err := iterGoTestEvents(jsonPath)
		if err != nil {
			return nil, err
		}
		for _, event := range events {
			output := event.Output
			test := event.Test

			for _, match := range apiMarkRE.FindAllStringSubmatch(output, -1) {
				if len(match) != 4 {
					continue
				}
				marks = append(marks, coverage.Mark{
					Key: coverage.RouteKey{
						Method: strings.ToUpper(match[2]),
						Path:   coverage.NormalizePath(match[3]),
					},
					Role: strings.ToLower(match[1]),
					Test: test,
				})
			}
		}
	}
	return marks, nil
}

// iterGoTestEvents 读取 go test -json 事件。
// 首选 json.Decoder，因为它天然支持连续 JSON 对象；如果输入混入普通文本，则退回逐行 JSON 解析。
func iterGoTestEvents(jsonPath string) ([]coverage.TestEvent, error) {
	content, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("read go test json %s: %w", jsonPath, err)
	}

	events, err := decodeJSONStream(content)
	if err == nil {
		return events, nil
	}
	return decodeJSONLines(content), nil
}

// decodeJSONStream 使用 json.Decoder 连续解析 JSON 对象流。
// go test -json 原始输出通常是一行一个 JSON；被格式化后也仍是多个连续 JSON 对象，Decoder 可以统一处理。
func decodeJSONStream(content []byte) ([]coverage.TestEvent, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	var events []coverage.TestEvent
	for {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				return events, nil
			}
			return nil, err
		}

		var event coverage.TestEvent
		if err := json.Unmarshal(raw, &event); err == nil {
			events = append(events, event)
			continue
		}

		var list []coverage.TestEvent
		if err := json.Unmarshal(raw, &list); err == nil {
			events = append(events, list...)
		}
	}
}

// decodeJSONLines 是容错路径，用于处理测试日志中夹杂非 JSON 行的情况。
// 能成功解析为 go test 事件的行会保留，无法解析的普通日志行会被忽略。
func decodeJSONLines(content []byte) []coverage.TestEvent {
	var events []coverage.TestEvent
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event coverage.TestEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		events = append(events, event)
	}
	return events
}
