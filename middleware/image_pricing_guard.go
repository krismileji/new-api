package middleware

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

// ImageGenerationPricingGuard checks the client's model before plugin alias
// resolution or channel selection. Image edits do not use this downstream gate.
func ImageGenerationPricingGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost || c.Request.URL.Path != "/v1/images/generations" {
			c.Next()
			return
		}
		request, err := getModelFromRequest(c)
		if err != nil {
			// Keep malformed-body handling with the existing protocol middleware.
			c.Next()
			return
		}
		modelName := common.GetStringIfEmpty(request.Model, "dall-e")
		if _, configured := ratio_setting.GetImageRatio(modelName); configured {
			c.Next()
			return
		}
		// Preserve the existing downstream API error contract, including HTTP 200.
		message := "image generation is currently not supported"
		service.RecordRequestPolicyTermination(c, types.NewErrorWithStatusCode(
			errors.New(message), types.ErrorCodeInvalidRequest, http.StatusOK, types.ErrOptionWithSkipRetry(),
		))
		abortWithOpenAiMessage(c, http.StatusOK, message, types.ErrorCodeInvalidRequest)
	}
}
