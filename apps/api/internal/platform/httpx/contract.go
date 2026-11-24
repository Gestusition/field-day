package httpx

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/gin-gonic/gin"

	"github.com/Gestusition/field-day/apps/api/internal/platform/auth"
)

// Contract enforces api/openapi.yaml on every API request:
//
//  1. finds the operation for the request,
//  2. applies the operation's security scheme (userAuth / adminAuth / deviceAuth),
//  3. validates path, query and body against the schema.
//
// Authentication runs before validation so unauthenticated callers learn
// nothing about the expected input.
type Contract struct {
	doc    *openapi3.T
	router routers.Router
	auth   *auth.Authenticator
	log    *slog.Logger
}

// NewContract builds the middleware from the embedded OpenAPI document.
func NewContract(doc *openapi3.T, a *auth.Authenticator, log *slog.Logger) (*Contract, error) {
	r, err := gorillamux.NewRouter(doc)
	if err != nil {
		return nil, fmt.Errorf("contract router: %w", err)
	}
	return &Contract{doc: doc, router: r, auth: a, log: log}, nil
}

// Middleware returns the gin handler.
func (ct *Contract) Middleware() gin.HandlerFunc {
	opts := &openapi3filter.Options{
		AuthenticationFunc: openapi3filter.NoopAuthenticationFunc, // done below
		MultiError:         true,
	}
	return func(c *gin.Context) {
		route, params, err := ct.router.FindRoute(c.Request)
		if err != nil {
			if errors.Is(err, routers.ErrMethodNotAllowed) {
				MethodNotAllowed(c)
			} else {
				NotFound(c)
			}
			return
		}

		if scheme := ct.scheme(route.Operation); scheme != "" {
			ctx, err := ct.auth.Authenticate(c.Request, scheme)
			if err != nil {
				WriteError(c, ct.log, err)
				return
			}
			c.Request = c.Request.WithContext(ctx)
		}

		in := &openapi3filter.RequestValidationInput{
			Request:    c.Request,
			PathParams: params,
			Route:      route,
			Options:    opts,
		}
		if err := openapi3filter.ValidateRequest(c.Request.Context(), in); err != nil {
			WriteProblem(c, http.StatusBadRequest, "validation_failed",
				"The request does not match the API contract.", validationFields(err))
			return
		}
		c.Next()
	}
}

func (ct *Contract) scheme(op *openapi3.Operation) string {
	reqs := ct.doc.Security
	if op.Security != nil {
		reqs = *op.Security
	}
	for _, req := range reqs {
		for name := range req {
			return name
		}
	}
	return ""
}

// validationFields flattens kin-openapi errors into field/message pairs.
func validationFields(err error) []FieldError {
	var out []FieldError
	var walk func(error)
	walk = func(err error) {
		var multi openapi3.MultiError
		if errors.As(err, &multi) {
			for _, e := range multi {
				walk(e)
			}
			return
		}
		var reqErr *openapi3filter.RequestError
		if errors.As(err, &reqErr) {
			field := "body"
			if reqErr.Parameter != nil {
				field = reqErr.Parameter.Name
			}
			var inner openapi3.MultiError
			if errors.As(reqErr.Err, &inner) {
				for _, e := range inner {
					out = append(out, schemaField(field, e))
				}
				return
			}
			out = append(out, schemaField(field, reqErr.Err))
			return
		}
		out = append(out, FieldError{Field: "request", Message: err.Error()})
	}
	walk(err)
	return out
}

func schemaField(base string, err error) FieldError {
	var se *openapi3.SchemaError
	if errors.As(err, &se) {
		if ptr := se.JSONPointer(); len(ptr) > 0 {
			return FieldError{Field: base + "." + strings.Join(ptr, "."), Message: se.Reason}
		}
		return FieldError{Field: base, Message: se.Reason}
	}
	if err == nil {
		return FieldError{Field: base, Message: "invalid"}
	}
	return FieldError{Field: base, Message: err.Error()}
}
