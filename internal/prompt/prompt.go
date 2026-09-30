// Package prompt builds the system prompt for the puruClaw agent.
//
// Architecture mirrors picoclaw pkg/agent/prompt.go + context.go:
// typed layers/slots/sources, a validating registry + stack, priority
// sorting, and legacy join with "\n\n---\n\n". Composition mirrors
// picoclaw ContextBuilder: kernel identity + workspace bootstrap
// (AGENTS.md, SOUL.md, USER.md) + skill catalog + active skills +
// memory + per-request runtime + summary. Paths follow the PURU layout
// where memory/MEMORY.md and memory/context/ live inside memory/ and
// skills live in skills/{skill-name}/SKILL.md.
package prompt

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/purujawa06-bot/PURU-AI/internal/workspace"
)

// PromptLayer groups prompt parts by stability: kernel first, turn last.
type PromptLayer string

const (
	PromptLayerKernel      PromptLayer = "kernel"
	PromptLayerInstruction PromptLayer = "instruction"
	PromptLayerCapability  PromptLayer = "capability"
	PromptLayerContext     PromptLayer = "context"
	PromptLayerTurn        PromptLayer = "turn"
)

// PromptSlot is the fine-grained position inside a layer.
type PromptSlot string

const (
	PromptSlotIdentity     PromptSlot = "identity"
	PromptSlotHierarchy    PromptSlot = "hierarchy"
	PromptSlotWorkspace    PromptSlot = "workspace"
	PromptSlotTooling      PromptSlot = "tooling"
	PromptSlotMCP          PromptSlot = "mcp"
	PromptSlotSkillCatalog PromptSlot = "skill_catalog"
	PromptSlotActiveSkill  PromptSlot = "active_skill"
	PromptSlotMemory       PromptSlot = "memory"
	PromptSlotRuntime      PromptSlot = "runtime"
	PromptSlotSummary      PromptSlot = "summary"
	PromptSlotMessage      PromptSlot = "message"
	PromptSlotSteering     PromptSlot = "steering"
	PromptSlotSubTurn      PromptSlot = "subturn"
	PromptSlotToolResult   PromptSlot = "tool_result"
	PromptSlotInterrupt    PromptSlot = "interrupt"
	PromptSlotOutput       PromptSlot = "output"
)

// PromptSourceID identifies which producer owns a prompt part.
type PromptSourceID string

const (
	PromptSourceKernel         PromptSourceID = "runtime.kernel"
	PromptSourceHierarchy      PromptSourceID = "runtime.hierarchy"
	PromptSourceWorkspace      PromptSourceID = "workspace.definition"
	PromptSourceRuntime        PromptSourceID = "runtime.context"
	PromptSourceSummary        PromptSourceID = "context.summary"
	PromptSourceMemory         PromptSourceID = "memory:workspace"
	PromptSourceSkillCatalog   PromptSourceID = "skill:index"
	PromptSourceActiveSkills   PromptSourceID = "skill:active"
	PromptSourceAgentDiscovery PromptSourceID = "agent:discovery"
	PromptSourceToolRegistry   PromptSourceID = "tool_registry:native"
	PromptSourceToolDiscovery  PromptSourceID = "tool_registry:discovery"
	PromptSourceOutputPolicy   PromptSourceID = "runtime.output"
	PromptSourceSubTurnProfile PromptSourceID = "subturn.profile"
	PromptSourceUserMessage    PromptSourceID = "turn:user_message"
	PromptSourceSteering       PromptSourceID = "turn:steering"
	PromptSourceSubTurnResult  PromptSourceID = "turn:subturn_result"
	PromptSourceToolResult     PromptSourceID = "turn:tool_result"
	PromptSourceInterrupt      PromptSourceID = "turn:interrupt"
)

// PromptCachePolicy hints provider cache behavior for a part.
type PromptCachePolicy string

const (
	PromptCacheDefault   PromptCachePolicy = ""
	PromptCacheEphemeral PromptCachePolicy = "ephemeral"
	PromptCacheNone      PromptCachePolicy = "none"
)

// PromptPlacement binds a source to one layer/slot pair.
type PromptPlacement struct {
	Layer PromptLayer
	Slot  PromptSlot
}

// PromptSourceDescriptor declares a valid producer and its placements.
type PromptSourceDescriptor struct {
	ID              PromptSourceID
	Owner           string
	Description     string
	Allowed         []PromptPlacement
	StableByDefault bool
}

// PromptSource marks the producer of one part.
type PromptSource struct {
	ID   PromptSourceID
	Name string
	Path string
}

// PromptPart is one composable block of the system prompt.
type PromptPart struct {
	ID      string
	Layer   PromptLayer
	Slot    PromptSlot
	Source  PromptSource
	Title   string
	Content string
	Stable  bool
	Cache   PromptCachePolicy
}

// Request describes one system prompt build.
type Request struct {
	Workspace string
	Memory    string
	Summary   string
	// TemplatePath overrides the build-prompt.md location.
	// Empty means ~/.puru/build-prompt.md.
	TemplatePath string

	Channel           string
	ChatID            string
	SenderID          string
	SenderDisplayName string

	// ActiveSkills merges with the frontmatter skills list (runtime use_skill state).
	ActiveSkills []string
	// Overlays are appended as turn-level parts (subturn profiles, steering).
	Overlays []PromptPart

	SuppressDefaultSystemPrompt bool
	SuppressSkillContext        bool
	SuppressToolUseRule         bool

	AllowedSkills []string
	AllowedTools  []string

	ToolUseFallback bool

	Policy workspace.SkillsPolicy
}

// PromptContributor extends the prompt with custom parts.
type PromptContributor interface {
	PromptSource() PromptSourceDescriptor
	ContributePrompt(req Request) ([]PromptPart, error)
}

// PromptRegistry validates placements and collects contributor parts.
type PromptRegistry struct {
	mu           sync.RWMutex
	sources      map[PromptSourceID]PromptSourceDescriptor
	contributors []PromptContributor
	warned       map[PromptSourceID]struct{}
}

// NewPromptRegistry returns a registry with builtin sources registered.
func NewPromptRegistry() *PromptRegistry {
	r := &PromptRegistry{
		sources: make(map[PromptSourceID]PromptSourceDescriptor),
		warned:  make(map[PromptSourceID]struct{}),
	}
	for _, desc := range builtinPromptSources() {
		if err := r.RegisterSource(desc); err != nil {
			log.Printf("[prompt] register source %q: %v", desc.ID, err)
		}
	}
	return r
}

func builtinPromptSources() []PromptSourceDescriptor {
	return []PromptSourceDescriptor{
		{
			ID:              PromptSourceKernel,
			Owner:           "agent",
			Description:     "Core puruClaw identity and hard rules",
			Allowed:         []PromptPlacement{{Layer: PromptLayerKernel, Slot: PromptSlotIdentity}},
			StableByDefault: true,
		},
		{
			ID:              PromptSourceHierarchy,
			Owner:           "agent",
			Description:     "Prompt hierarchy rules",
			Allowed:         []PromptPlacement{{Layer: PromptLayerKernel, Slot: PromptSlotHierarchy}},
			StableByDefault: true,
		},
		{
			ID:              PromptSourceWorkspace,
			Owner:           "workspace",
			Description:     "Workspace and agent definition files",
			Allowed:         []PromptPlacement{{Layer: PromptLayerInstruction, Slot: PromptSlotWorkspace}},
			StableByDefault: true,
		},
		{
			ID:              PromptSourceToolDiscovery,
			Owner:           "tools",
			Description:     "Tool discovery instructions",
			Allowed:         []PromptPlacement{{Layer: PromptLayerCapability, Slot: PromptSlotTooling}},
			StableByDefault: true,
		},
		{
			ID:              PromptSourceToolRegistry,
			Owner:           "tools",
			Description:     "Native provider tool definitions",
			Allowed:         []PromptPlacement{{Layer: PromptLayerCapability, Slot: PromptSlotTooling}},
			StableByDefault: true,
		},
		{
			ID:              PromptSourceSkillCatalog,
			Owner:           "skills",
			Description:     "Installed skill catalog",
			Allowed:         []PromptPlacement{{Layer: PromptLayerCapability, Slot: PromptSlotSkillCatalog}},
			StableByDefault: true,
		},
		{
			ID:              PromptSourceActiveSkills,
			Owner:           "skills",
			Description:     "Active skill instructions for the current request",
			Allowed:         []PromptPlacement{{Layer: PromptLayerCapability, Slot: PromptSlotActiveSkill}},
			StableByDefault: false,
		},
		{
			ID:              PromptSourceAgentDiscovery,
			Owner:           "agent",
			Description:     "Structured multi-agent discovery registry",
			Allowed:         []PromptPlacement{{Layer: PromptLayerCapability, Slot: PromptSlotTooling}},
			StableByDefault: false,
		},
		{
			ID:              PromptSourceMemory,
			Owner:           "memory",
			Description:     "Workspace memory context",
			Allowed:         []PromptPlacement{{Layer: PromptLayerContext, Slot: PromptSlotMemory}},
			StableByDefault: true,
		},
		{
			ID:              PromptSourceRuntime,
			Owner:           "agent",
			Description:     "Per-request runtime context",
			Allowed:         []PromptPlacement{{Layer: PromptLayerContext, Slot: PromptSlotRuntime}},
			StableByDefault: false,
		},
		{
			ID:              PromptSourceSummary,
			Owner:           "context_manager",
			Description:     "Conversation summary context",
			Allowed:         []PromptPlacement{{Layer: PromptLayerContext, Slot: PromptSlotSummary}},
			StableByDefault: false,
		},
		{
			ID:              PromptSourceOutputPolicy,
			Owner:           "agent",
			Description:     "Output formatting policy",
			Allowed:         []PromptPlacement{{Layer: PromptLayerContext, Slot: PromptSlotOutput}},
			StableByDefault: true,
		},
		{
			ID:              PromptSourceSubTurnProfile,
			Owner:           "subturn",
			Description:     "Child agent profile instructions",
			Allowed:         []PromptPlacement{{Layer: PromptLayerInstruction, Slot: PromptSlotWorkspace}},
			StableByDefault: false,
		},
		{
			ID:              PromptSourceUserMessage,
			Owner:           "turn",
			Description:     "Current user message for this turn",
			Allowed:         []PromptPlacement{{Layer: PromptLayerTurn, Slot: PromptSlotMessage}},
			StableByDefault: false,
		},
		{
			ID:              PromptSourceSteering,
			Owner:           "turn",
			Description:     "Steering message injected into a running turn",
			Allowed:         []PromptPlacement{{Layer: PromptLayerTurn, Slot: PromptSlotSteering}},
			StableByDefault: false,
		},
		{
			ID:              PromptSourceSubTurnResult,
			Owner:           "turn",
			Description:     "SubTurn result injected into a parent turn",
			Allowed:         []PromptPlacement{{Layer: PromptLayerTurn, Slot: PromptSlotSubTurn}},
			StableByDefault: false,
		},
		{
			ID:              PromptSourceInterrupt,
			Owner:           "turn",
			Description:     "Graceful interrupt hint injected into the terminal LLM call",
			Allowed:         []PromptPlacement{{Layer: PromptLayerTurn, Slot: PromptSlotInterrupt}},
			StableByDefault: false,
		},
	}
}

// RegisterSource adds or replaces a source descriptor.
func (r *PromptRegistry) RegisterSource(desc PromptSourceDescriptor) error {
	if r == nil {
		return fmt.Errorf("prompt registry is nil")
	}
	desc.ID = PromptSourceID(strings.TrimSpace(string(desc.ID)))
	if desc.ID == "" {
		return fmt.Errorf("prompt source id is required")
	}
	if len(desc.Allowed) == 0 {
		return fmt.Errorf("prompt source %q must declare at least one placement", desc.ID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sources[desc.ID] = clonePromptSourceDescriptor(desc)
	return nil
}

// RegisterContributor adds a contributor, replacing the same source id.
func (r *PromptRegistry) RegisterContributor(contributor PromptContributor) error {
	if r == nil {
		return fmt.Errorf("prompt registry is nil")
	}
	if contributor == nil {
		return fmt.Errorf("prompt contributor is nil")
	}
	desc := contributor.PromptSource()
	desc.ID = PromptSourceID(strings.TrimSpace(string(desc.ID)))
	if err := r.RegisterSource(desc); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.contributors = slices.DeleteFunc(r.contributors, func(existing PromptContributor) bool {
		return PromptSourceID(strings.TrimSpace(string(existing.PromptSource().ID))) == desc.ID
	})
	r.contributors = append(r.contributors, contributor)
	return nil
}

// Collect gathers contributor parts for one request.
func (r *PromptRegistry) Collect(req Request) ([]PromptPart, error) {
	if r == nil {
		return nil, nil
	}
	r.mu.RLock()
	contributors := append([]PromptContributor(nil), r.contributors...)
	r.mu.RUnlock()
	var parts []PromptPart
	for _, contributor := range contributors {
		contributed, err := contributor.ContributePrompt(req)
		if err != nil {
			return nil, err
		}
		for _, part := range contributed {
			if err := r.ValidatePart(part); err != nil {
				return nil, err
			}
			parts = append(parts, part)
		}
	}
	return parts, nil
}

// ValidatePart rejects parts written to a slot their source does not own.
func (r *PromptRegistry) ValidatePart(part PromptPart) error {
	if r == nil {
		return nil
	}
	sourceID := PromptSourceID(strings.TrimSpace(string(part.Source.ID)))
	if sourceID == "" {
		return fmt.Errorf("prompt part %q has empty source id", part.ID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	desc, ok := r.sources[sourceID]
	if !ok {
		if _, warned := r.warned[sourceID]; !warned {
			r.warned[sourceID] = struct{}{}
			log.Printf("[prompt] unregistered source %q allowed in compatibility mode", sourceID)
		}
		return nil
	}
	if promptPlacementAllowed(desc.Allowed, PromptPlacement{Layer: part.Layer, Slot: part.Slot}) {
		return nil
	}
	return fmt.Errorf("prompt source %q cannot write to %s/%s", sourceID, part.Layer, part.Slot)
}

func promptPlacementAllowed(allowed []PromptPlacement, placement PromptPlacement) bool {
	return slices.ContainsFunc(allowed, func(candidate PromptPlacement) bool {
		return candidate.Layer == placement.Layer && candidate.Slot == placement.Slot
	})
}

func clonePromptSourceDescriptor(desc PromptSourceDescriptor) PromptSourceDescriptor {
	desc.Allowed = append([]PromptPlacement(nil), desc.Allowed...)
	return desc
}

// PromptStack accumulates validated parts until sealed.
type PromptStack struct {
	registry *PromptRegistry
	parts    []PromptPart
	sealed   bool
}

// NewPromptStack returns an empty stack bound to the registry.
func NewPromptStack(registry *PromptRegistry) *PromptStack {
	return &PromptStack{registry: registry}
}

// Add appends one part, skipping empty content like picoclaw.
func (s *PromptStack) Add(part PromptPart) error {
	if s == nil {
		return fmt.Errorf("prompt stack is nil")
	}
	if s.sealed {
		return fmt.Errorf("prompt stack is sealed")
	}
	if strings.TrimSpace(part.Content) == "" {
		return nil
	}
	if strings.TrimSpace(part.ID) == "" {
		return fmt.Errorf("prompt part id is required")
	}
	if s.registry != nil {
		if err := s.registry.ValidatePart(part); err != nil {
			return err
		}
	}
	s.parts = append(s.parts, part)
	return nil
}

// Seal freezes the stack against further writes.
func (s *PromptStack) Seal() {
	if s != nil {
		s.sealed = true
	}
}

// Parts returns a copy of the accumulated parts.
func (s *PromptStack) Parts() []PromptPart {
	if s == nil || len(s.parts) == 0 {
		return nil
	}
	return append([]PromptPart(nil), s.parts...)
}

var defaultRegistry = NewPromptRegistry()

// ToolUseRule renders the instruction forcing real tool calls.
// The text lives in build-prompt.md so it stays editable.
func ToolUseRule() string {
	return renderBuildPromptSection(defaultBuildPrompt, "tool_use_rule", nil)
}

func toolUseRuleWithTemplate(templateText string) string {
	out := renderBuildPromptSection(templateText, "tool_use_rule", nil)
	if strings.TrimSpace(out) != "" {
		return out
	}
	return renderBuildPromptSection(defaultBuildPrompt, "tool_use_rule", nil)
}

func getIdentityWithTemplate(templateText, workspacePath string, includeToolUseRule bool, includeOnboardingRule bool) string {
	rules := []string{}
	if includeToolUseRule {
		rules = append(rules, toolUseRuleWithTemplate(templateText))
	}
	accuracyData := map[string]any{"IncludeToolUseRule": includeToolUseRule}
	rules = append(rules,
		renderBuildPromptSection(templateText, "accuracy_rule", accuracyData),
		renderBuildPromptSection(templateText, "context_summary_rule", nil),
	)
	if includeOnboardingRule {
		rules = append(rules,
			renderBuildPromptSection(templateText, "onboarding_rule", nil),
		)
	}
	if includeToolUseRule {
		rules = append(rules, renderBuildPromptSection(templateText, "memory_rule", map[string]any{
			"Workspace": workspacePath,
		}))
	}
	rules = append(rules,
		renderBuildPromptSection(templateText, "reply_language_rule", nil),
		renderBuildPromptSection(templateText, "workspace_rule", nil),
	)
	numbered := make([]string, 0, len(rules))
	for i, rule := range rules {
		trimmed := strings.TrimSpace(rule)
		if trimmed == "" {
			continue
		}
		numbered = append(numbered, fmt.Sprintf("%d. %s", len(numbered)+1, trimmed))
		_ = i
	}
	return renderBuildPromptSection(templateText, "identity", map[string]any{
		"Workspace": workspacePath,
		"Rules":     strings.Join(numbered, "\n\n"),
	})
}

func getIdentity(workspacePath string, includeToolUseRule bool, includeOnboardingRule bool) string {
	return getIdentityWithTemplate(loadBuildPromptText(""), workspacePath, includeToolUseRule, includeOnboardingRule)
}

// onboardingPlaceholderToken marks unfilled profile slots in the workspace
// bootstrap files (AGENTS.md, SOUL.md, USER.md) and long-term memory.
const onboardingPlaceholderToken = "PLACEHOLDER"

// hasOnboardingPlaceholders reports whether any profile source still
// contains the placeholder token, meaning the user has not finished
// onboarding yet. A blank memory argument falls back to the MEMORY.md
// file so fresh workspaces are detected even when no memory is passed.
func hasOnboardingPlaceholders(def workspace.Definition, memory, workspacePath string) bool {
	for _, source := range []string{def.AgentsBody, def.Soul, def.User, memory} {
		if strings.Contains(source, onboardingPlaceholderToken) {
			return true
		}
	}
	if strings.TrimSpace(memory) == "" && strings.TrimSpace(workspacePath) != "" {
		if data, err := os.ReadFile(workspace.MemoryPath(workspacePath)); err == nil {
			if strings.Contains(string(data), onboardingPlaceholderToken) {
				return true
			}
		}
	}
	return false
}

func formatSenderLine(senderID, senderDisplayName string) string {
	senderID = strings.TrimSpace(senderID)
	senderDisplayName = strings.TrimSpace(senderDisplayName)
	switch {
	case senderDisplayName != "" && senderID != "":
		return fmt.Sprintf("Current sender: %s (ID: %s)", senderDisplayName, senderID)
	case senderDisplayName != "":
		return fmt.Sprintf("Current sender: %s", senderDisplayName)
	case senderID != "":
		return fmt.Sprintf("Current sender: %s", senderID)
	default:
		return ""
	}
}

func buildDynamicContextWithTemplate(templateText, channel, chatID, senderID, senderDisplayName string) string {
	now := time.Now().Format("2006-01-02 15:04 (Monday)")
	rt := fmt.Sprintf("%s %s, Go %s", runtime.GOOS, runtime.GOARCH, runtime.Version())
	senderLine := formatSenderLine(senderID, senderDisplayName)
	return renderBuildPromptSection(templateText, "runtime_section", map[string]any{
		"CurrentTime": now,
		"Runtime":     rt,
		"HasSession":  strings.TrimSpace(channel) != "" && strings.TrimSpace(chatID) != "",
		"Channel":     channel,
		"ChatID":      chatID,
		"HasSender":   strings.TrimSpace(senderLine) != "",
		"SenderLine":  senderLine,
	})
}

func buildDynamicContext(channel, chatID, senderID, senderDisplayName string) string {
	return buildDynamicContextWithTemplate(loadBuildPromptText(""), channel, chatID, senderID, senderDisplayName)
}

func buildMemoryContentWithTemplate(templateText, memory string) string {
	guidance := renderBuildPromptSection(templateText, "memory_guidance", nil)
	view := strings.TrimSpace(memory)
	if view == "" {
		view = "(empty)"
	}
	return renderBuildPromptSection(templateText, "memory_section", map[string]any{
		"MemoryGuidance": guidance,
		"MemoryView":     view,
	})
}

func buildMemoryContent(memory string) string {
	return buildMemoryContentWithTemplate(loadBuildPromptText(""), memory)
}

func buildSummaryContentWithTemplate(templateText, summary string) string {
	prefix := renderBuildPromptSection(templateText, "summary_prefix", nil)
	return renderBuildPromptSection(templateText, "summary_section", map[string]any{
		"SummaryPrefix": prefix,
		"Summary":       summary,
	})
}

func buildSummaryContent(summary string) string {
	return buildSummaryContentWithTemplate(loadBuildPromptText(""), summary)
}

func skillCatalogIntroWithTemplate(templateText string, includeToolUse bool) string {
	return renderBuildPromptSection(templateText, "skill_catalog_intro", map[string]any{
		"IncludeToolUse": includeToolUse,
	})
}

func skillCatalogSectionWithTemplate(templateText, intro, catalog string) string {
	return renderBuildPromptSection(templateText, "skill_catalog_section", map[string]any{
		"Intro":   intro,
		"Catalog": catalog,
	})
}

func activeSkillsSectionWithTemplate(templateText, bodies string) string {
	return renderBuildPromptSection(templateText, "active_skills_section", map[string]any{
		"Bodies": bodies,
	})
}

func promptAllowsTool(req Request, name string) bool {
	if len(req.AllowedTools) == 0 {
		return true
	}
	allowed := cleanAllowedSet(req.AllowedTools)
	_, ok := allowed[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

func filterNamesByAllowed(names []string, allowed []string) []string {
	if len(names) == 0 {
		return nil
	}
	allowedSet := cleanAllowedSet(allowed)
	if len(allowedSet) == 0 {
		return nil
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if _, ok := allowedSet[strings.ToLower(strings.TrimSpace(name))]; ok {
			out = append(out, name)
		}
	}
	return out
}

func cleanAllowedSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		out[value] = struct{}{}
	}
	return out
}

func effectiveSkillsPolicy(req Request) workspace.SkillsPolicy {
	if req.SuppressSkillContext {
		return workspace.SkillsPolicy{Mode: workspace.SkillsModeOff}
	}
	if len(req.AllowedSkills) == 0 {
		return req.Policy
	}
	base := req.Policy
	if workspace.NormalizeSkillsMode(base.Mode) == workspace.SkillsModeOff {
		return workspace.SkillsPolicy{Mode: workspace.SkillsModeOff}
	}
	if workspace.NormalizeSkillsMode(base.Mode) == workspace.SkillsModeCustom {
		allowed := filterNamesByAllowed(base.Allow, req.AllowedSkills)
		if len(allowed) == 0 {
			allowed = filterNamesByAllowed(req.AllowedSkills, base.Allow)
		}
		return workspace.SkillsPolicy{Mode: workspace.SkillsModeCustom, Allow: allowed}
	}
	return workspace.SkillsPolicy{Mode: workspace.SkillsModeCustom, Allow: append([]string(nil), req.AllowedSkills...)}
}

func resolveActiveSkills(req Request, frontmatter []string) []string {
	names := workspace.MergeActiveSkills(frontmatter, req.ActiveSkills)
	if len(req.AllowedSkills) > 0 {
		names = filterNamesByAllowed(names, req.AllowedSkills)
	}
	return names
}

// Build renders the full system prompt for one request.
// Section texts come from build-prompt.md (see template.go); the layer and
// slot ordering below stays in code so overlays keep a stable position.
func Build(req Request) (string, error) {
	templateText := loadBuildPromptText(req.TemplatePath)
	if strings.TrimSpace(req.Workspace) != "" {
		// Best-effort restore: never overwrites existing user edits.
		_ = workspace.Ensure(req.Workspace)
	}
	if req.SuppressDefaultSystemPrompt {
		var fallback []string
		for _, overlay := range sortPromptParts(req.Overlays) {
			if strings.TrimSpace(overlay.Content) == "" {
				continue
			}
			fallback = append(fallback, overlay.Content)
		}
		if len(fallback) == 0 && req.ToolUseFallback {
			fallback = append(fallback, toolUseRuleWithTemplate(templateText))
		}
		return strings.Join(fallback, "\n\n---\n\n"), nil
	}

	def := workspace.Load(req.Workspace)
	if strings.TrimSpace(def.AgentsBody) == "" {
		def.AgentsLabel = workspace.FileAgents
		def.AgentsBody = workspace.DefaultAgentsMD
	}
	if strings.TrimSpace(def.Soul) == "" {
		def.Soul = workspace.DefaultSoulMD
	}
	if strings.TrimSpace(def.User) == "" {
		def.User = workspace.DefaultUserMD
	}

	includeToolUseRule := !req.SuppressToolUseRule
	includeOnboardingRule := hasOnboardingPlaceholders(def, req.Memory, req.Workspace)
	policy := effectiveSkillsPolicy(req)

	stack := NewPromptStack(defaultRegistry)
	add := func(part PromptPart) {
		if err := stack.Add(part); err != nil {
			log.Printf("[prompt] skip part %q: %v", part.ID, err)
		}
	}

	add(PromptPart{
		ID:      "kernel.identity",
		Layer:   PromptLayerKernel,
		Slot:    PromptSlotIdentity,
		Source:  PromptSource{ID: PromptSourceKernel, Name: "identity"},
		Title:   "puruClaw identity",
		Content: getIdentityWithTemplate(templateText, req.Workspace, includeToolUseRule, includeOnboardingRule),
		Stable:  true,
		Cache:   PromptCacheEphemeral,
	})

	if bootstrap := def.Bootstrap(); bootstrap != "" {
		add(PromptPart{
			ID:      "instruction.workspace",
			Layer:   PromptLayerInstruction,
			Slot:    PromptSlotWorkspace,
			Source:  PromptSource{ID: PromptSourceWorkspace, Name: "workspace"},
			Title:   "workspace instructions",
			Content: bootstrap,
			Stable:  true,
			Cache:   PromptCacheEphemeral,
		})
	}

	if !req.SuppressSkillContext {
		activeNames := resolveActiveSkills(req, def.FrontmatterSkills)
		if catalog := workspace.BuildSkillsSummaryExcluding(req.Workspace, policy, activeNames); catalog != "" {
			intro := skillCatalogIntroWithTemplate(templateText, includeToolUseRule && promptAllowsTool(req, "use_skill"))
			add(PromptPart{
				ID:      "capability.skill_catalog",
				Layer:   PromptLayerCapability,
				Slot:    PromptSlotSkillCatalog,
				Source:  PromptSource{ID: PromptSourceSkillCatalog, Name: "skill:index"},
				Title:   "skill catalog",
				Content: skillCatalogSectionWithTemplate(templateText, intro, catalog),
				Stable:  true,
				Cache:   PromptCacheEphemeral,
			})
		}
		if bodies := workspace.LoadSkillsForContext(req.Workspace, activeNames, policy); bodies != "" {
			add(PromptPart{
				ID:      "capability.active_skills",
				Layer:   PromptLayerCapability,
				Slot:    PromptSlotActiveSkill,
				Source:  PromptSource{ID: PromptSourceActiveSkills, Name: "skill:active"},
				Title:   "active skills",
				Content: activeSkillsSectionWithTemplate(templateText, bodies),
				Stable:  false,
				Cache:   PromptCacheNone,
			})
		}
	}

	add(PromptPart{
		ID:      "context.memory",
		Layer:   PromptLayerContext,
		Slot:    PromptSlotMemory,
		Source:  PromptSource{ID: PromptSourceMemory, Name: "memory:workspace"},
		Title:   "memory",
		Content: buildMemoryContentWithTemplate(templateText, req.Memory),
		Stable:  true,
		Cache:   PromptCacheEphemeral,
	})

	add(PromptPart{
		ID:      "context.runtime",
		Layer:   PromptLayerContext,
		Slot:    PromptSlotRuntime,
		Source:  PromptSource{ID: PromptSourceRuntime, Name: "runtime"},
		Title:   "runtime context",
		Content: buildDynamicContextWithTemplate(templateText, req.Channel, req.ChatID, req.SenderID, req.SenderDisplayName),
		Stable:  false,
		Cache:   PromptCacheNone,
	})

	if strings.TrimSpace(req.Summary) != "" {
		add(PromptPart{
			ID:      "context.summary",
			Layer:   PromptLayerContext,
			Slot:    PromptSlotSummary,
			Source:  PromptSource{ID: PromptSourceSummary, Name: "context.summary"},
			Title:   "context summary",
			Content: buildSummaryContentWithTemplate(templateText, req.Summary),
			Stable:  false,
			Cache:   PromptCacheNone,
		})
	}

	for _, overlay := range sortPromptParts(req.Overlays) {
		if strings.TrimSpace(overlay.Content) == "" {
			continue
		}
		if err := defaultRegistry.ValidatePart(overlay); err != nil {
			log.Printf("[prompt] skip overlay %q: %v", overlay.ID, err)
			continue
		}
		add(overlay)
	}

	if contributed, err := defaultRegistry.Collect(req); err != nil {
		return "", err
	} else {
		for _, part := range sortPromptParts(contributed) {
			if strings.TrimSpace(part.Content) == "" {
				continue
			}
			add(part)
		}
	}

	stack.Seal()
	return renderPromptPartsLegacy(stack.Parts()), nil
}

// Get renders the system prompt with the workspace path, the memory file and
// the latest conversation summary ("" when none). It self-heals first:
// missing bootstrap files are recreated from embedded defaults so the
// physical files always exist, then bootstrap and skill catalog are loaded.
// The policy gates skill injection (off suppresses every skill section,
// custom restricts to the allowlist).
func Get(memory string, summary string, workspacePath string, policy workspace.SkillsPolicy) (string, error) {
	return Build(Request{
		Workspace: workspacePath,
		Memory:    memory,
		Summary:   summary,
		Policy:    policy,
	})
}

func renderPromptPartsLegacy(parts []PromptPart) string {
	textParts := make([]string, 0, len(parts))
	for _, part := range sortPromptParts(parts) {
		if strings.TrimSpace(part.Content) == "" {
			continue
		}
		textParts = append(textParts, part.Content)
	}
	return strings.Join(textParts, "\n\n---\n\n")
}

func sortPromptParts(parts []PromptPart) []PromptPart {
	sorted := append([]PromptPart(nil), parts...)
	slices.SortStableFunc(sorted, func(a, b PromptPart) int {
		if d := layerPriority(b.Layer) - layerPriority(a.Layer); d != 0 {
			return d
		}
		if d := slotPriority(b.Slot) - slotPriority(a.Slot); d != 0 {
			return d
		}
		if a.Source.ID != b.Source.ID {
			return strings.Compare(string(a.Source.ID), string(b.Source.ID))
		}
		return strings.Compare(a.ID, b.ID)
	})
	return sorted
}

func layerPriority(layer PromptLayer) int {
	switch layer {
	case PromptLayerKernel:
		return 100
	case PromptLayerInstruction:
		return 80
	case PromptLayerCapability:
		return 60
	case PromptLayerContext:
		return 40
	case PromptLayerTurn:
		return 20
	default:
		return 0
	}
}

func slotPriority(slot PromptSlot) int {
	switch slot {
	case PromptSlotIdentity:
		return 1000
	case PromptSlotHierarchy:
		return 990
	case PromptSlotWorkspace:
		return 900
	case PromptSlotTooling:
		return 800
	case PromptSlotMCP:
		return 790
	case PromptSlotSkillCatalog:
		return 780
	case PromptSlotActiveSkill:
		return 770
	case PromptSlotMemory:
		return 700
	case PromptSlotOutput:
		return 695
	case PromptSlotRuntime:
		return 690
	case PromptSlotSummary:
		return 680
	case PromptSlotMessage:
		return 600
	case PromptSlotSteering:
		return 590
	case PromptSlotSubTurn:
		return 580
	case PromptSlotInterrupt:
		return 570
	default:
		return 0
	}
}
