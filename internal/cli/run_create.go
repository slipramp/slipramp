package cli

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/cli/remote"
)

type runCreateOptions struct {
	serverURL      string
	format         string
	target         string
	action         string
	idempotencyKey string
	parameters     parameterAssignments
}

func (a App) newRunCreateCommand(exitCode *int) *cobra.Command {
	options := runCreateOptions{parameters: parameterAssignments{}}
	command := &cobra.Command{
		Use:   "create",
		Short: "Create a Run",
		Args: func(_ *cobra.Command, arguments []string) error {
			if len(arguments) != 0 {
				return errors.New("run create does not accept positional arguments")
			}

			return nil
		},
		Run: func(_ *cobra.Command, _ []string) {
			*exitCode = a.runRunCreate(options)
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.serverURL, "server", "", "Slipramp Server URL (default: SLIPRAMP_URL)")
	flags.StringVar(&options.target, "target", "", "exact Target UUID (required)")
	flags.StringVar(&options.action, "action", "", "Action name (required)")
	flags.Var(&options.parameters, "set", "parameter NAME=VALUE (repeatable)")
	flags.StringVar(&options.idempotencyKey, "idempotency-key", "", "stable request key (default: fresh random UUID)")
	flags.StringVar(&options.format, "format", "text", "output format: text or json")

	return command
}

func (a App) runRunCreate(options runCreateOptions) int {
	if err := validateRemoteFormat(options.format); err != nil {
		return a.invocationError(err.Error())
	}
	targetID, err := uuid.Parse(options.target)
	if err != nil {
		return a.invocationError("--target must be an exact Target UUID")
	}
	if options.action == "" {
		return a.invocationError("--action is required")
	}
	if options.idempotencyKey == "" {
		options.idempotencyKey = uuid.NewString()
	} else if !utf8.ValidString(options.idempotencyKey) || strings.TrimSpace(options.idempotencyKey) == "" ||
		utf8.RuneCountInString(options.idempotencyKey) > 128 ||
		strings.IndexFunc(options.idempotencyKey, unicode.IsControl) >= 0 {
		return a.invocationError("--idempotency-key must contain 1 to 128 characters")
	}

	client, err := a.remoteClient(options.serverURL)
	if err != nil {
		return a.invocationError(err.Error())
	}
	target, exitCode := a.loadRunTarget(client, targetID)
	if exitCode != ExitValid {
		return exitCode
	}
	action, found := target.Definition.Spec.Actions[options.action]
	if !found {
		return a.runValidationError(fmt.Errorf(
			"action %q is not defined by Target %s",
			options.action,
			targetID,
		))
	}
	if action.Inputs != nil && len(*action.Inputs) != 0 {
		return a.runValidationError(errors.New(
			"this Action requires artifact inputs; open the producing Run in the Web UI to start it",
		))
	}

	declarations := catalogParameters(action.Parameters)
	values, err := catalog.ParseParameterValues(declarations, options.parameters)
	if err != nil {
		return a.runValidationError(err)
	}
	requestParameters, err := apiParameterValues(values)
	if err != nil {
		return a.internalError("encode Run parameters", err)
	}
	body := api.RunCreate{
		TargetID:   targetID,
		Action:     options.action,
		Parameters: &requestParameters,
	}
	requestOptions := api.CreateRunParams{IdempotencyKey: &options.idempotencyKey}
	response, err := client.API.CreateRun(a.commandContext(), &requestOptions, body)
	if err != nil {
		return a.remoteError(client.NetworkError("create Run", err))
	}
	defer func() { _ = response.Body.Close() }()
	parsed, err := api.ParseCreateRunResponse(response)
	if err != nil {
		return a.remoteError(client.InvalidResponse("create Run", err))
	}
	if parsed.StatusCode() != http.StatusCreated || parsed.JSON201 == nil {
		return a.remoteError(client.ResponseError("create Run", parsed.StatusCode(), parsed.Body))
	}

	if options.format == "json" {
		if err := renderRemoteJSON(a.Stdout, parsed.JSON201); err != nil {
			return a.internalError("write created Run", err)
		}
	} else if err := renderCreatedRunText(a.Stdout, *parsed.JSON201); err != nil {
		return a.internalError("write created Run", err)
	}

	return ExitValid
}

func (a App) loadRunTarget(client *remote.Client, targetID uuid.UUID) (api.Target, int) {
	response, err := client.API.GetTarget(a.commandContext(), targetID)
	if err != nil {
		return api.Target{}, a.remoteError(client.NetworkError("get Target", err))
	}
	defer func() { _ = response.Body.Close() }()
	parsed, err := api.ParseGetTargetResponse(response)
	if err != nil {
		return api.Target{}, a.remoteError(client.InvalidResponse("get Target", err))
	}
	if parsed.StatusCode() != http.StatusOK || parsed.JSON200 == nil {
		return api.Target{}, a.remoteError(client.ResponseError("get Target", parsed.StatusCode(), parsed.Body))
	}
	if parsed.JSON200.ArchivedAt != nil {
		return api.Target{}, a.runValidationError(fmt.Errorf("target %s is archived", targetID))
	}

	return *parsed.JSON200, ExitValid
}

func catalogParameters(parameters *map[string]api.Parameter) map[string]catalog.Parameter {
	if parameters == nil {
		return map[string]catalog.Parameter{}
	}
	result := make(map[string]catalog.Parameter, len(*parameters))
	for name, parameter := range *parameters {
		converted := catalog.Parameter{Type: string(parameter.Type)}
		if parameter.Title != nil {
			converted.Title = *parameter.Title
		}
		if parameter.Description != nil {
			converted.Description = *parameter.Description
		}
		if parameter.Required != nil {
			converted.Required = *parameter.Required
		}
		if parameter.Values != nil {
			converted.Values = slices.Clone(*parameter.Values)
		}
		if parameter.Environment != nil {
			converted.Environment = *parameter.Environment
		}
		if parameter.Argument != nil {
			converted.Argument = &catalog.ParameterArgument{}
			if parameter.Argument.Name != nil {
				converted.Argument.Name = *parameter.Argument.Name
			}
		}
		result[name] = converted
	}

	return result
}

func apiParameterValues(values map[string]any) (map[string]api.ParameterValue, error) {
	result := make(map[string]api.ParameterValue, len(values))
	for name, value := range values {
		var converted api.ParameterValue
		var err error
		switch typed := value.(type) {
		case string:
			err = converted.FromParameterValue0(typed)
		case bool:
			err = converted.FromParameterValue1(typed)
		case float64:
			err = converted.FromParameterValue2(typed)
		default:
			err = fmt.Errorf("unsupported normalized type %T", value)
		}
		if err != nil {
			return nil, fmt.Errorf("parameter %q: %w", name, err)
		}
		result[name] = converted
	}

	return result, nil
}

func (a App) runValidationError(err error) int {
	if _, writeErr := fmt.Fprintf(a.Stderr, "slipramp: %v\n", err); writeErr != nil {
		return ExitInternal
	}

	return ExitInvalid
}
