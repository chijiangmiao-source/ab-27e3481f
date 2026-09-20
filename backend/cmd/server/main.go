package main

import (
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"

	"wafer-audit/backend/internal/audit"

	"github.com/gin-gonic/gin"
)

// auditRequest 前端提交的两幅二值方阵文本。
type auditRequest struct {
	Reference string `json:"reference"`
	Recheck   string `json:"recheck"`
}

// auditErrorResponse 校验失败/计算异常时返回；前端据此清空旧结论。
type auditErrorResponse struct {
	Error  string             `json:"error"`
	Errors []audit.FieldError `json:"errors,omitempty"`
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	if mode := os.Getenv("GIN_MODE"); mode != "" {
		gin.SetMode(mode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "wafer-audit-backend"})
	})

	api := r.Group("/api")
	{
		api.GET("/meta", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"minSide":     audit.MinSide,
				"maxSide":     audit.MaxSide,
				"poseOrder":   audit.PoseName[:],
				"shiftsRange": "dy,dx in [-(n-1), n-1]",
			})
		})

		api.POST("/audit", handleAudit)
	}

	log.Printf("wafer-audit backend listening on :%s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

func handleAudit(c *gin.Context) {
	// 限制请求体，避免异常大输入拖垮解析。
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4<<20)

	var req auditRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, auditErrorResponse{
			Error: "请求体不是合法的 JSON，或体积超过 4MiB 限制",
		})
		return
	}

	// 两个输入分别解析，错误精确定位到具体输入框（可同时报两处）。
	var errs []audit.FieldError
	ref, nRef, errRef := audit.ParseMatrix("reference", req.Reference)
	if errRef != nil {
		errs = append(errs, *errRef)
	}
	chk, nChk, errChk := audit.ParseMatrix("recheck", req.Recheck)
	if errChk != nil {
		errs = append(errs, *errChk)
	}
	if len(errs) == 0 && nRef != nChk {
		errs = append(errs, audit.FieldError{
			Field:   "recheck",
			Code:    "size_mismatch",
			Message: "两幅方阵边长不一致：参考图边长为 " + strconv.Itoa(nRef) +
				"，复检图边长为 " + strconv.Itoa(nChk),
			Width:  nChk,
			Expect: nRef,
		})
	}
	if len(errs) > 0 {
		c.JSON(http.StatusUnprocessableEntity, auditErrorResponse{
			Error:  "输入校验失败，错误已定位到具体输入",
			Errors: errs,
		})
		return
	}

	// 计算阶段任何异常都转成 500，绝不返回上一次的旧结论（本服务无状态，
	// 前端在收到非成功响应时也会清空结果区）；修正输入后可原样重试。
	result, err := safeAudit(ref, chk, nRef)
	if err != nil {
		c.JSON(http.StatusInternalServerError, auditErrorResponse{
			Error: "审计计算异常，本次未产生任何结论，请修正后原样重试：" + err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, result)
}

func safeAudit(ref, chk []byte, n int) (result *audit.Result, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = errors.New("内部计算异常")
			log.Printf("audit panic recovered: %v", rec)
		}
	}()
	return audit.Audit(ref, chk, n), nil
}
