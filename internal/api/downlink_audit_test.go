package api

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
)

// 每一条直连下发都必须登记理由，避免新入口绕过 outbox 且无人评审。
func TestDownlinkExemptionsHaveReasons(t *testing.T) {
	want := map[uint16]bool{
		0x8104: true, 0x8106: true, 0x8107: true, 0x8201: true,
		0x8608: true, 0x8702: true, 0x8802: true, 0x9205: true,
		0x8203: true,
	}
	require.Len(t, downlinkExemptions, len(want))
	for msgID := range want {
		reason, ok := downlinkExemptions[msgID]
		require.True(t, ok, "缺少豁免登记: 0x%04X", msgID)
		require.NotEmpty(t, strings.TrimSpace(reason), "豁免理由为空: 0x%04X", msgID)
	}
	for msgID, reason := range downlinkExemptions {
		require.True(t, want[msgID], "未预期的豁免项: 0x%04X", msgID)
		require.NotEmpty(t, strings.TrimSpace(reason))
	}
}

// 除 outbox 与豁免清单的封装函数外，API 包内不得出现裸的 serv.Send。
func TestNoRawServSendOutsideHelpers(t *testing.T) {
	allowed := map[string]bool{
		"dispatchDownlink":       true,
		"enqueueDownlinkCommand": true,
		"directSend":             true,
	}
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	fset := token.NewFileSet()
	scanned := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, parseErr := parser.ParseFile(fset, file, nil, 0)
		require.NoError(t, parseErr, "parse %s", file)
		scanned++
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || allowed[fn.Name.Name] {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "Send" {
					return true
				}
				ident, ok := selector.X.(*ast.Ident)
				if ok && ident.Name == "serv" {
					t.Errorf("%s: %s 内出现裸 serv.Send，必须走 dispatchDownlink 或 directSend", file, fn.Name.Name)
				}
				return true
			})
		}
	}
	require.Positive(t, scanned, "未扫描到任何源文件")
}

func TestWriteCommandAcceptedMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		command    *contracts.DownlinkResult
		created    bool
		wantStatus int
		wantID     bool
		wantDup    bool
	}{
		{
			name:       "新建命令返回 202 与 commandId",
			command:    &contracts.DownlinkResult{ID: "cmd-1"},
			created:    true,
			wantStatus: http.StatusAccepted,
			wantID:     true,
			wantDup:    false,
		},
		{
			name:       "幂等命中返回 200 与 duplicate=true",
			command:    &contracts.DownlinkResult{ID: "cmd-1"},
			created:    false,
			wantStatus: http.StatusOK,
			wantID:     true,
			wantDup:    true,
		},
		{
			name:       "off 模式无记录返回 202 且不伪造 commandId",
			command:    nil,
			created:    false,
			wantStatus: http.StatusAccepted,
			wantID:     false,
			wantDup:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			writeCommandAccepted(c, tt.command, tt.created, gin.H{"messageId": "0x8103"})

			require.Equal(t, tt.wantStatus, recorder.Code)
			var body map[string]any
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			_, hasID := body["commandId"]
			require.Equal(t, tt.wantID, hasID)
			require.Equal(t, tt.wantDup, body["duplicate"] == true)
		})
	}
}
