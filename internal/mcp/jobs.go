// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	sdo "github.com/scrape-do/sdo-go"
)

// The async job tools: the q.scrape.do job API (create, poll, fetch, list,
// cancel, account). Everything the API returns about a job or a task can carry
// text from outside this server (the target URLs, a task's error message, a
// page's content), so it reaches the model inside an untrusted block; only the
// ids, counts and states the server itself reports are in its own voice.

// noCreditNote tells the model a call costs nothing, as creditNote tells it
// one does.
const noCreditNote = "Spends no Scrape.do credits."

// jobOptionsSchema is the job body options a create tool passes through, with
// the API's own PascalCase keys. The SDK refuses Targets and Plugin in it.
var jobOptionsSchema = map[string]any{"type": "object",
	"description": "Job body options, with the async API's own PascalCase keys: Super, GeoCode, " +
		"RegionalGeoCode, Device, Render (an object such as {\"WaitUntil\": \"networkidle0\"}), Output, " +
		"Timeout, Method, Body, Headers, WebhookURL, WebhookHeaders, ... (see " +
		"https://scrape.do/documentation/async-api/create-job/)."}

// jobIDSchema and taskIDSchema are the ids the job and task tools take.
var (
	jobIDSchema  = map[string]any{"type": "string", "minLength": 1, "description": "Job id, as returned by a create tool."}
	taskIDSchema = map[string]any{"type": "string", "minLength": 1, "description": "Task id, from the job's TaskIDs."}
)

var jobTools = []tool{
	{
		name: "async_job_create",
		description: "Submit an async Scrape.do job for one or more target URLs; it returns the job id and one " +
			"task id per URL at once, and the scraping runs in the background on the account's separate async " +
			"concurrency pool. Poll async_job_status, then fetch each task with async_task_result." + creditNote,
		schema: object(map[string]any{
			"targets": map[string]any{"type": "array", "description": "Target URLs, one task each.",
				"items": map[string]any{"type": "string", "minLength": 1}},
			"options": jobOptionsSchema,
		}, "targets"),
		run: func(ctx context.Context, s Scraper, args map[string]any) map[string]any {
			raw := args["targets"].([]any)
			targets := make([]string, len(raw))
			for i, t := range raw {
				targets[i] = t.(string)
			}
			options, _ := args["options"].(map[string]any)
			job, err := s.Jobs().Create(ctx, targets, options)
			return jobCreated(s, job, err)
		},
	},
	{
		name: "async_plugin_job_create",
		description: "Submit an async Scrape.do plugin job: one task per params entry (up to 1000), each with the " +
			"plugin's own parameters; it returns the job id and task ids at once. Poll async_job_status, then " +
			"fetch each task with async_task_result." + creditNote + " Keys follow " +
			"https://scrape.do/documentation/async-api/plugins/ (amazon/pdp, amazon/search, google/search, " +
			"google/shopping, google/maps/search, walmart/store, ...).",
		schema: object(map[string]any{
			"plugin": map[string]any{"type": "string", "minLength": 1, "description": "Plugin key, e.g. amazon/pdp."},
			"params": map[string]any{"type": "array",
				"description": "One object per task, with the plugin's own parameter names.",
				"items":       map[string]any{"type": "object"}},
			"options": jobOptionsSchema,
		}, "plugin", "params"),
		run: func(ctx context.Context, s Scraper, args map[string]any) map[string]any {
			raw := args["params"].([]any)
			params := make([]map[string]any, len(raw))
			for i, p := range raw {
				params[i] = p.(map[string]any)
			}
			options, _ := args["options"].(map[string]any)
			job, err := s.Jobs().CreatePlugin(ctx, args["plugin"].(string), params, options)
			return jobCreated(s, job, err)
		},
	},
	{
		name:        "async_job_status",
		description: "Show an async job's state (queuing, queued, pending, rotating, success, error, canceled) and its tasks. " + noCreditNote,
		schema:      object(map[string]any{"jobId": jobIDSchema}, "jobId"),
		run: func(ctx context.Context, s Scraper, args map[string]any) map[string]any {
			st, err := s.Jobs().Status(ctx, args["jobId"].(string))
			if err != nil {
				return failure(s, "async_job_status", err)
			}
			return toolResult(s, false, fmt.Sprintf("job: %s\nstate: %s\ntasks: %d", st.JobID, st.Status, len(st.Tasks)),
				untrusted(s, string(jsonText(toJSONValue(st)))))
		},
	},
	{
		name: "async_task_result",
		description: "Fetch one finished task of an async job: its state, the target's status code, and the " +
			"delivered content, fenced as untrusted third-party data. A task that has not finished yet reports " +
			"its state only. " + noCreditNote,
		schema: object(map[string]any{"jobId": jobIDSchema, "taskId": taskIDSchema}, "jobId", "taskId"),
		run:    taskResultTool,
	},
	{
		name: "async_jobs_list",
		description: "List the account's async jobs a page at a time (default: the 10 most recent), optionally " +
			"filtered by state and start time and sorted; the reply says how many pages there are. " + noCreditNote,
		schema: object(map[string]any{
			"page":     map[string]any{"type": "integer", "description": "Page number, from 1."},
			"pageSize": map[string]any{"type": "integer", "description": "Jobs per page, 1 to 100 (default 10)."},
			"status": map[string]any{"type": "string", "description": "Only jobs in this state.",
				"enum": []string{"queuing", "queued", "pending", "rotating", "success", "error", "canceled"}},
			"startFrom": map[string]any{"type": "string", "description": "Only jobs started at or after this RFC 3339 time, e.g. 2025-01-01T00:00:00Z."},
			"startTo":   map[string]any{"type": "string", "description": "Only jobs started at or before this RFC 3339 time."},
			"sort": map[string]any{"type": "string", "description": "Newest first (default) or oldest first.",
				"enum": []string{"start_time_desc", "start_time_asc"}},
		}),
		check: func(args map[string]any) string {
			_, why := listOptionsFromArgs(args)
			return why
		},
		run: func(ctx context.Context, s Scraper, args map[string]any) map[string]any {
			opts, _ := listOptionsFromArgs(args)
			page, err := s.Jobs().List(ctx, opts)
			if err != nil {
				return failure(s, "async_jobs_list", err)
			}
			return toolResult(s, false, fmt.Sprintf("page %d of %d (%d jobs in total, %d on this page)",
				page.PageNumber, page.TotalPages, page.TotalCount, len(page.Jobs)),
				untrusted(s, string(jsonText(toJSONValue(page.Jobs)))))
		},
	},
	{
		name:        "async_job_cancel",
		description: "Cancel an async job that has not finished. " + noCreditNote,
		schema:      object(map[string]any{"jobId": jobIDSchema}, "jobId"),
		run: func(ctx context.Context, s Scraper, args map[string]any) map[string]any {
			id := args["jobId"].(string)
			if err := s.Jobs().Cancel(ctx, id); err != nil {
				return failure(s, "async_job_cancel", err)
			}
			return toolResult(s, false, "canceled job "+id)
		},
	},
	{
		name:        "async_account",
		description: "Show the account's async concurrency (total and free), active jobs and remaining credits. " + noCreditNote,
		schema:      object(map[string]any{}),
		run: func(ctx context.Context, s Scraper, _ map[string]any) map[string]any {
			acc, err := s.Jobs().Me(ctx)
			if err != nil {
				return failure(s, "async_account", err)
			}
			return toolResult(s, false, fmt.Sprintf("concurrency: %d free of %d\nactive jobs: %d\ncredits: %d",
				acc.FreeConcurrency, acc.TotalConcurrency, acc.ActiveJobs, acc.AvailableCredits))
		},
	},
}

func jobCreated(s Scraper, job *sdo.Job, err error) map[string]any {
	if err != nil {
		return failure(s, "job create", err)
	}
	text := fmt.Sprintf("job: %s\ntasks: %d", job.JobID, len(job.TaskIDs))
	for _, id := range job.TaskIDs {
		text += "\n- " + id
	}
	return toolResult(s, false, text)
}

func taskResultTool(ctx context.Context, s Scraper, args map[string]any) map[string]any {
	tr, err := s.Jobs().Result(ctx, args["jobId"].(string), args["taskId"].(string))
	if err != nil {
		return failure(s, "async_task_result", err)
	}
	meta := fmt.Sprintf("task: %s\nstate: %s", tr.TaskID, tr.Status)
	switch tr.Status {
	case sdo.JobStatusSuccess:
	case sdo.JobStatusError, sdo.JobStatusCanceled:
		texts := []string{meta}
		if tr.ErrorMessage != "" {
			texts = append(texts, untrusted(s, "error: "+tr.ErrorMessage))
		}
		return toolResult(s, true, texts...)
	default:
		return toolResult(s, false, meta+"\nThe task has not finished yet; call async_task_result again later.")
	}
	meta += "\nstatus: " + strconv.Itoa(tr.StatusCode)
	body, err := tr.ContentBytes()
	if err != nil {
		return failure(s, "async_task_result", err)
	}
	if kind, ok := binaryType(tr.ResponseHeaders["Content-Type"]); ok || tr.Base64EncodedContent {
		if !ok {
			kind = "binary"
		}
		return toolResult(s, false, meta, fmt.Sprintf("[%s content, %d bytes: not shown, this tool returns text only]",
			kind, len(body)))
	}
	texts := []string{meta}
	if tr.URL != "" {
		texts = append(texts, untrusted(s, "url: "+tr.URL))
	}
	return toolResult(s, false, append(texts, untrusted(s, string(body)))...)
}

// listOptionsFromArgs maps validated list arguments to the SDK's options; the
// reason is non-empty for a value the schema cannot rule out (a bad time).
func listOptionsFromArgs(args map[string]any) (*sdo.ListJobsOptions, string) {
	opts := &sdo.ListJobsOptions{}
	for name, dst := range map[string]*int{"page": &opts.Page, "pageSize": &opts.PageSize} {
		if v, ok := args[name].(json.Number); ok {
			n, err := strconv.Atoi(string(v))
			if err != nil || n < 1 {
				return nil, fmt.Sprintf("%s must be a positive integer", name)
			}
			*dst = n
		}
	}
	if opts.PageSize > 100 {
		return nil, "pageSize must be 1 to 100"
	}
	opts.Status, _ = args["status"].(string)
	opts.Sort, _ = args["sort"].(string)
	for name, dst := range map[string]*time.Time{"startFrom": &opts.StartFrom, "startTo": &opts.StartTo} {
		if v, ok := args[name].(string); ok {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return nil, fmt.Sprintf("%s must be an RFC 3339 time such as 2025-01-01T00:00:00Z", name)
			}
			*dst = t
		}
	}
	return opts, ""
}

// toJSONValue round-trips a typed value through JSON so jsonText can render
// it like plugin output (decoded generically, numbers as float64).
func toJSONValue(v any) any {
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}
