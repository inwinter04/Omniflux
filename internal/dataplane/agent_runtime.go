package dataplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maximhq/bifrost/core/schemas"

	"github.com/inwinter04/omniflux/internal/asset"
)

const maxAgentMessageBytes = 64 * 1024

type AgentInputError struct {
	Code    string
	Message string
}

func (e *AgentInputError) Error() string { return e.Message }

func agentInputError(code, message string) error {
	return &AgentInputError{Code: code, Message: message}
}

type AgentActor struct {
	Channel       string
	UserID        *uuid.UUID
	ProjectID     *uuid.UUID
	ApplicationID *uuid.UUID
	APIKeyID      *uuid.UUID
}

type AgentRunStart struct {
	OrganizationID uuid.UUID
	AssetID        uuid.UUID
	VersionID      uuid.UUID
	SessionID      uuid.UUID
	Variables      map[string]any
	Message        string
	Actor          AgentActor
	PublishedOnly  bool
}

type AgentRunContext struct {
	RunID          uuid.UUID
	SessionID      uuid.UUID
	OrganizationID uuid.UUID
	RequestText    string
	VersionID      uuid.UUID
	Version        int
	Spec           asset.AgentSpec
	PromptSpec     asset.PromptSpec
	RenderedPrompt string
	Input          []schemas.ChatMessage
	CreatedSession bool
}

func AgentChatParameters(spec asset.AgentSpec, prompt asset.PromptSpec) *schemas.ChatParameters {
	params := &schemas.ChatParameters{Temperature: spec.Temperature, TopP: spec.TopP}
	if spec.MaxTokens > 0 {
		params.MaxCompletionTokens = &spec.MaxTokens
	}
	if format, ok := asset.StructuredResponseFormat(prompt); ok {
		params.ResponseFormat = &format
	}
	return params
}

func AgentTextMessage(role schemas.ChatMessageRole, content string) schemas.ChatMessage {
	value := schemas.ChatMessageContent{ContentStr: &content}
	return schemas.ChatMessage{Role: role, Content: &value}
}

func (d Deps) StartAgentRun(ctx context.Context, input AgentRunStart) (AgentRunContext, error) {
	var result AgentRunContext
	input.Message = strings.TrimSpace(input.Message)
	result.OrganizationID = input.OrganizationID
	result.RequestText = input.Message
	if input.Message == "" {
		return result, agentInputError("agent_message_required", "Message is required.")
	}
	if len(input.Message) > maxAgentMessageBytes {
		return result, agentInputError("agent_message_too_long", "Message exceeds 64 KB.")
	}
	if input.Actor.Channel != "debug" && input.Actor.Channel != "api" {
		return result, agentInputError("agent_actor_invalid", "Invalid session actor.")
	}
	err := pgx.BeginFunc(ctx, d.Pool, func(tx pgx.Tx) error {
		var rawSpec, rawPromptSpec json.RawMessage
		variables := input.Variables
		if variables == nil {
			variables = map[string]any{}
		}
		if input.SessionID == uuid.Nil {
			if err := queryInitialAgentVersion(ctx, tx, input, &result, &rawSpec); err != nil {
				return err
			}
		} else {
			if len(input.Variables) > 0 {
				return agentInputError("agent_session_variables_immutable", "Existing sessions cannot change initial variables. Create a new session.")
			}
			if err := queryExistingAgentSession(ctx, tx, input, &result, &rawSpec, &variables); err != nil {
				return err
			}
		}
		spec, err := asset.ParseAgentSpec(rawSpec)
		if err != nil {
			return fmt.Errorf("Agent configuration is invalid: %w", err)
		}
		result.Spec = spec
		promptID, _ := uuid.Parse(spec.PromptTemplateID)
		promptVersionID, _ := parseUUIDOrNil(spec.PromptVersionID)
		if err := tx.QueryRow(ctx, `
			SELECT v.spec FROM asset_versions v
			JOIN assets a ON a.id = v.asset_id
			WHERE v.id = $1 AND a.id = $2 AND a.organization_id = $3 AND a.asset_type = 'prompt_template'
		`, promptVersionID, promptID, input.OrganizationID).Scan(&rawPromptSpec); err != nil {
			return fmt.Errorf("failed to read the prompt version referenced by the agent: %w", err)
		}
		prompt, err := asset.ParsePromptSpec(rawPromptSpec)
		if err != nil {
			return fmt.Errorf("prompt configuration referenced by the agent is invalid: %w", err)
		}
		result.PromptSpec = prompt
		result.RenderedPrompt, err = asset.RenderPrompt(prompt, variables)
		if err != nil {
			return agentInputError("agent_variables_invalid", "Agent variables are invalid.")
		}
		result.Input = []schemas.ChatMessage{AgentTextMessage(schemas.ChatMessageRoleSystem, result.RenderedPrompt)}
		if input.SessionID != uuid.Nil {
			rows, err := tx.Query(ctx, `
				SELECT role, content FROM (
					SELECT role, content, created_at, id FROM agent_messages WHERE session_id = $1
					ORDER BY created_at DESC, id DESC LIMIT $2
				) recent ORDER BY created_at ASC, id ASC
			`, input.SessionID, spec.MaxHistoryMessages)
			if err != nil {
				return err
			}
			for rows.Next() {
				var role, content string
				if err := rows.Scan(&role, &content); err != nil {
					rows.Close()
					return err
				}
				result.Input = append(result.Input, AgentTextMessage(schemas.ChatMessageRole(role), content))
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
		}
		result.Input = append(result.Input, AgentTextMessage(schemas.ChatMessageRoleUser, input.Message))
		result.RunID = uuid.New()
		modelServiceID, _ := uuid.Parse(spec.ModelServiceID)
		actorType := "user"
		if input.Actor.Channel == "api" {
			actorType = "api_key"
		}
		if input.SessionID == uuid.Nil {
			result.SessionID = uuid.New()
			title := []rune(input.Message)
			if len(title) > 40 {
				title = title[:40]
			}
			rawVariables, _ := json.Marshal(variables)
			if _, err := tx.Exec(ctx, `
				INSERT INTO agent_sessions(id, organization_id, asset_id, agent_version_id, channel, title, variables,
				                           created_by, project_id, application_id, api_key_id)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			`, result.SessionID, input.OrganizationID, input.AssetID, result.VersionID, input.Actor.Channel, string(title), rawVariables,
				input.Actor.UserID, input.Actor.ProjectID, input.Actor.ApplicationID, input.Actor.APIKeyID); err != nil {
				return err
			}
			result.CreatedSession = true
		} else {
			result.SessionID = input.SessionID
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO agent_runs(id, organization_id, session_id, asset_id, version_id, model_service_id,
			                       actor_type, actor_user_id, project_id, application_id, api_key_id, request_text, rendered_prompt)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		`, result.RunID, input.OrganizationID, result.SessionID, input.AssetID, result.VersionID, modelServiceID,
			actorType, input.Actor.UserID, input.Actor.ProjectID, input.Actor.ApplicationID, input.Actor.APIKeyID,
			input.Message, result.RenderedPrompt); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agent_messages(id, session_id, agent_run_id, role, content) VALUES ($1,$2,$3,'user',$4)`,
			uuid.New(), result.SessionID, result.RunID, input.Message); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE agent_sessions SET updated_at = now() WHERE id = $1`, result.SessionID)
		return err
	})
	return result, err
}

func queryInitialAgentVersion(ctx context.Context, tx pgx.Tx, input AgentRunStart, result *AgentRunContext, rawSpec *json.RawMessage) error {
	var err error
	if input.VersionID != uuid.Nil {
		err = tx.QueryRow(ctx, `
			SELECT v.id, v.version, v.spec FROM asset_versions v
			JOIN assets a ON a.id = v.asset_id
			WHERE v.id = $1 AND a.id = $2 AND a.organization_id = $3 AND a.asset_type = 'agent'
			  AND (NOT $4 OR a.status = 'published')
		`, input.VersionID, input.AssetID, input.OrganizationID, input.PublishedOnly).Scan(&result.VersionID, &result.Version, rawSpec)
	} else {
		versionColumn := "latest_version"
		if input.PublishedOnly {
			versionColumn = "current_version_id"
		}
		query := fmt.Sprintf(`
			SELECT v.id, v.version, v.spec FROM assets a
			JOIN asset_versions v ON v.asset_id = a.id AND %s
			WHERE a.id = $1 AND a.organization_id = $2 AND a.asset_type = 'agent'
			  AND (NOT $3 OR a.status = 'published')
		`, map[string]string{"latest_version": "v.version = a.latest_version", "current_version_id": "v.id = a.current_version_id"}[versionColumn])
		err = tx.QueryRow(ctx, query, input.AssetID, input.OrganizationID, input.PublishedOnly).Scan(&result.VersionID, &result.Version, rawSpec)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return agentInputError("agent_version_not_found", "Agent version does not exist or is not published.")
	}
	return err
}

func queryExistingAgentSession(ctx context.Context, tx pgx.Tx, input AgentRunStart, result *AgentRunContext, rawSpec *json.RawMessage, variables *map[string]any) error {
	var rawVariables []byte
	err := tx.QueryRow(ctx, `
		SELECT s.variables, v.id, v.version, v.spec FROM agent_sessions s
		JOIN assets a ON a.id = s.asset_id
		JOIN asset_versions v ON v.id = s.agent_version_id
		WHERE s.id = $1 AND s.organization_id = $2 AND s.asset_id = $3 AND s.channel = $4
		  AND (($4 = 'debug' AND s.created_by = $5) OR ($4 = 'api' AND s.project_id = $6))
		  AND (NOT $7 OR a.status = 'published')
		FOR UPDATE OF s
	`, input.SessionID, input.OrganizationID, input.AssetID, input.Actor.Channel, input.Actor.UserID, input.Actor.ProjectID,
		input.PublishedOnly).Scan(&rawVariables, &result.VersionID, &result.Version, rawSpec)
	if errors.Is(err, pgx.ErrNoRows) {
		return agentInputError("agent_session_not_found", "Agent session does not exist, is disabled, or is not accessible.")
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(rawVariables, variables); err != nil {
		return fmt.Errorf("failed to read session variables: %w", err)
	}
	return nil
}

func (d Deps) FinishAgentRun(ctx context.Context, run AgentRunContext, status string, result DebugResult, runErr error) error {
	ctx = context.WithoutCancel(ctx)
	if result.Currency == "" {
		_ = d.Pool.QueryRow(ctx, `SELECT cost_currency FROM model_candidates WHERE model_service_id = $1 LIMIT 1`, run.Spec.ModelServiceID).Scan(&result.Currency)
	}
	if result.Currency == "" {
		result.Currency = "USD"
	}
	errorMessage := ""
	if runErr != nil {
		errorMessage = runErr.Error()
	}
	err := pgx.BeginFunc(ctx, d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE agent_runs SET status=$2, response_text=$3, error_message=$4, provider=$5, upstream_model=$6,
			                      input_tokens=$7, output_tokens=$8, cached_tokens=$9, reasoning_tokens=$10,
			                      cost=$11, cost_currency=$14, first_token_ms=$12, total_ms=$13, finished_at=now()
			WHERE id=$1
		`, run.RunID, status, result.Response, nullableAgentString(errorMessage), nullableAgentString(result.Provider), nullableAgentString(result.UpstreamModel),
			result.InputTokens, result.OutputTokens, result.CachedTokens, result.ReasoningTokens, result.Cost,
			result.FirstTokenMS, result.TotalMS, result.Currency); err != nil {
			return err
		}
		if result.Response != "" {
			if _, err := tx.Exec(ctx, `INSERT INTO agent_messages(id, session_id, agent_run_id, role, content) VALUES ($1,$2,$3,'assistant',$4)`,
				uuid.New(), run.SessionID, run.RunID, result.Response); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE agent_sessions SET updated_at = now() WHERE id = $1`, run.SessionID)
		return err
	})
	return err
}

func parseUUIDOrNil(value string) (uuid.UUID, error) {
	return uuid.Parse(value)
}

func nullableAgentString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
