# AI Agent Implementation Remaining Work

## Current status

The main implementation has already been landed across:

- `easydo-server`: AI schema, models, routes, handlers, pipeline AI task types, AI session creation path
- `easydo-agent`: Eino-based `ai-task` execution path with structured outputs
- `easydo-frontend`: Store AI Provider/Model management, Settings AI Agent Definition/Version management, pipeline detail AI output rendering

What remains is the final runtime verification and completion pass.

---

## Remaining items and execution plan

### 1. Confirm compose runtime health after rebuild

**Goal**
- Confirm all rebuilt services are healthy after Docker rebuild.

**Scope**
- `frontend`
- `server`
- `server2`
- `agent`
- `agent2`

**Plan**
- Check `docker compose ps`
- Inspect service logs for any restart loops or startup failures
- Treat `server2` as a blocker only if the issue is reproducible and caused by this feature change
- If the issue is pre-existing or unrelated, document it instead of expanding scope

---

### 2. Run browser-based verification

**Goal**
- Verify the new UI surfaces are available and usable in the rebuilt stack.

**Plan**
- Log in through the running frontend
- Open **Store** and verify the new AI Provider / Model management page loads
- Open **Settings** and verify the new AI Agent Definition / Version management section loads
- Verify edit controls are only visible/usable for:
  - system admin
  - workspace owner
- Verify non-owner / non-admin role behavior is view-only or denied as intended

---

### 3. Run API verification against live Docker stack

**Goal**
- Verify the new backend APIs are reachable and behave correctly.

**Plan**
- Verify Store AI APIs:
  - list providers
  - create/update/delete provider
  - list/create/update/delete model
- Verify AI Definition APIs:
  - list definitions
  - create/update/delete definition
  - list/create/update/delete version
- Verify pipeline task type endpoint returns:
  - `mr_quality_check`
  - `requirement_defect_assistant`
- Verify permission failures are correct for unauthorized roles

---

### 4. Run end-to-end pipeline verification

**Goal**
- Verify the two new AI pipeline node types work through the real pipeline chain.

**Plan**
- Create or update a pipeline containing:
  - `mr_quality_check`
  - `requirement_defect_assistant`
- Trigger a pipeline run
- Confirm server-side artifacts are created correctly:
  - pipeline run node snapshot
  - agent task
  - AI session
- Confirm agent receives and executes the `ai-task`
- Confirm structured result is written back through:
  - task result data
  - pipeline outputs
  - pipeline detail rendering

---

### 5. Handle runtime configuration blockers explicitly

**Goal**
- Avoid hiding environment problems behind implementation claims.

**Plan**
- If end-to-end AI execution is blocked by missing provider credentials or runtime config, record exactly what is missing
- Verify everything else up to the blocking boundary
- Do not invent hidden provider configuration
- Do not claim end-to-end success unless the runtime config is actually present and verified

---

### 6. Oracle final review

**Goal**
- Run a final post-implementation review after runtime verification.

**Plan**
- Ask Oracle to review the completed implementation after:
  - Docker verification
  - browser verification
  - API verification
  - pipeline verification
- Fix only issues caused by this change
- Keep pre-existing unrelated issues documented, not absorbed into scope

---

### 7. Final delivery summary

**Goal**
- Produce a complete final report for handoff/checking.

**Plan**
- Summarize changed files and modules
- Summarize verification evidence
- Note any remaining blockers
- Explicitly separate:
  - completed implementation
  - environment-level blockers
  - pre-existing unrelated issues

---

## Important constraints for the remaining work

- Follow `AGENTS.md`
- Do **not** use host-machine Node/Go build commands for verification
- Use Docker / compose for build, run, and debugging validation
- Browser verification before API-only completion claims
- No runtime auto-migration shortcuts
- Agent Definition editing must remain restricted to:
  - system admin
  - workspace owner
