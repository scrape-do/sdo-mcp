// SPDX-License-Identifier: Apache-2.0

package mcp

// toolMeta is the display title and behavior hints tools/list reports for a
// tool (MCP ToolAnnotations). Clients use the hints to decide which calls run
// without asking; the Claude and ChatGPT directories require a title and the
// applicable hints on every tool.
type toolMeta struct {
	title                            string
	readOnly, destructive, openWorld bool
}

// toolMetas holds the annotations of every tool, by name. scrape and
// scrape_plugin change no state (they fetch a page, with the cost shown in
// every result) and reach the public internet; the create tools start a job;
// cancelling a job cannot be undone.
var toolMetas = map[string]toolMeta{
	"scrape":                  {title: "Scrape a web page", readOnly: true, openWorld: true},
	"scrape_plugin":           {title: "Run a Scrape.do plugin", readOnly: true, openWorld: true},
	"async_job_create":        {title: "Create async scrape job", openWorld: true},
	"async_plugin_job_create": {title: "Create async plugin job", openWorld: true},
	"async_job_status":        {title: "Get async job status", readOnly: true},
	"async_task_result":       {title: "Get async task result", readOnly: true},
	"async_jobs_list":         {title: "List async jobs", readOnly: true},
	"async_job_cancel":        {title: "Cancel async job", destructive: true},
	"async_account":           {title: "Get async account info", readOnly: true},
}

// annotations renders m as the MCP ToolAnnotations object.
func (m toolMeta) annotations() map[string]any {
	return map[string]any{
		"title":           m.title,
		"readOnlyHint":    m.readOnly,
		"destructiveHint": m.destructive,
		"openWorldHint":   m.openWorld,
	}
}
