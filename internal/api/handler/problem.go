package handler

import (
	"encoding/json"
	"net/http"

	"github.com/slipramp/slipramp/internal/api"
)

const blankProblemType = "about:blank"

func NewProblem(status int, detail string) api.Problem {
	problem := api.Problem{
		Type:   blankProblemType,
		Title:  http.StatusText(status),
		Status: status,
	}

	if detail != "" {
		problem.Detail = &detail
	}

	return problem
}

func WriteProblem(
	writer http.ResponseWriter,
	status int,
	detail string,
) {
	writer.Header().Set("Content-Type", "application/problem+json")
	writer.WriteHeader(status)

	problem := NewProblem(status, detail)
	_ = json.NewEncoder(writer).Encode(problem)
}
