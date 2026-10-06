# Claude Code guide and Rye & Rent game plan

Notes collected from a Claude Code session. They cover:

1. [A practical Claude Code guide](#part-1--claude-code-guide)
2. [Rye & Rent: design and build plan for a farm economy game](#part-2--rye--rent-game-plan)
3. [Recovering after an accidental Esc Esc, and working without git](#part-3--rewind-recovery-and-working-without-git)
4. [One prompt to build a Three.js browser UI for the game](#part-4--threejs-browser-ui-prompt)

The guide also exists as web pages (private, owner only):

- Claude Code Field Guide: https://claude.ai/artifact/1RfVQo1xrSmn8VB12uPVe1
- Rye & Rent Build Plan: https://claude.ai/artifact/UMegJXfUZWZgjTEHBQUNzD

Claude Code changes often. For the current reference, see the
[Claude Code documentation](https://docs.claude.com/en/docs/claude-code/overview),
or ask Claude Code itself: "how do I … in Claude Code?"

---

## Part 1 — Claude Code guide

### Why one prompt stalls

When you ask for a whole project at once, Claude has to guess dozens of
decisions you never stated. It writes everything in one go, so when something
is off you can't tell which part broke, and each fix risks breaking something
else.

| One prompt | The loop |
|---|---|
| "Make me a brick breaker game with levels, power-ups, sound, high scores and mobile controls." | "Read GAME.md. Plan milestone 1 only: a paddle and a bouncing ball. Don't build yet." |
| Claude picks the art style, controls, physics and file layout for you. | Decisions are written down once in a spec and in CLAUDE.md. |
| Hundreds of lines appear at once, none of them checked. | Each milestone is small, visible, and tested before the next. |
| The first bug you report touches code you never read. | Every working state is saved, so mistakes cost minutes. |

### The working loop

Run this once per milestone, not once per project.

1. **Explore** — Claude reads the project and asks you questions.
2. **Plan** — plan mode proposes steps. You edit them before any code.
3. **Build** — one milestone, small enough to check in a minute.
4. **Check** — run it, look at it, test it. Feed back what's wrong.
5. **Commit / back up** — save a working state, clear context, start the next loop.

### Setup

| Where | Good for |
|---|---|
| Desktop app, Code tab | Visual diffs, built-in browser preview, several sessions side by side |
| Terminal (`claude`) | Every slash command and settings dialog, scripting, fastest keyboard flow |
| IDE extension | VS Code and JetBrains, with Claude's edits shown inside your editor |

Install on macOS or Linux:

```sh
curl -fsSL https://claude.ai/install.sh | bash
```

Start a session in a folder. Claude only sees files inside that folder.

```sh
mkdir brick-breaker && cd brick-breaker
claude
```

In the desktop app, `/config`, `/permissions`, `/hooks` and `/doctor` open
terminal-only dialogs. Run `claude` in a terminal for those, or ask Claude in
plain words ("add a hook that formats JS files after edits").

### Everyday controls

| Input | What it does |
|---|---|
| `Shift` + `Tab` | Cycle permission modes: normal, accept edits, plan mode |
| `Esc` | Stop Claude mid-task. Work so far stays. Type a correction and continue |
| `Esc` `Esc` | Open rewind: jump back to an earlier prompt and restore code, conversation, or both. With text in the input box, it clears the text instead |
| `@file` | Point Claude at a file or folder: `@game.js` |
| `!command` | Run a shell command yourself; output goes into the conversation |
| `/` | List slash commands and skills |
| Paste an image | Screenshots of bugs or mockups. Drag in, or `Ctrl` + `V` in the terminal |
| `↑` | Recall earlier prompts |

**Permission modes**

| Mode | Behaviour | Use it |
|---|---|---|
| Normal | Asks before editing files and running commands | While learning, or on unfamiliar code |
| Accept edits | Edits files freely, still asks before shell commands | Once a plan is approved |
| Plan mode | Read-only. Claude researches and proposes a plan | At the start of every milestone |
| Auto mode | Acts without asking; risky actions are still checked and blocked | Longer runs you trust (availability depends on plan) |

### Writing prompts that work

A good prompt reads like a short ticket:

| Part | Example |
|---|---|
| Goal | Add three levels with different brick layouts. |
| Context | Layouts live in `@game.js`; see how level 1 is built. |
| Constraints | Store layouts as data, not code. Don't change the physics. |
| Done when | Clearing all bricks loads the next level, and tests pass. |

- **Ask questions first** when unsure: "Before coding, ask me anything that's unclear."
- **Ask for options** on design choices: "Give me three ways to handle ball speed-up, with trade-offs."
- **Ask it to think harder** on tricky logic, or pick a stronger model with `/model`.
- **Interrupt early.** If the first lines look wrong, press `Esc` and redirect.
- **Say how to verify.** "Run the tests" or "check it in the browser" lets Claude catch its own mistakes.

### CLAUDE.md: the project's memory

Each session starts with no memory of the last one. CLAUDE.md is a Markdown
file Claude loads automatically. `/init` writes a first draft from the code.

| File | Applies to |
|---|---|
| `./CLAUDE.md` | This project. Commit it so it's shared |
| `./sub/CLAUDE.md` | Loaded when Claude works in that folder |
| `~/.claude/CLAUDE.md` | All your projects. Personal preferences |

- Keep it short. Every line is read every session.
- Reference other files with `@path`.
- Edit with `/memory`, or say "add to CLAUDE.md that…".
- When Claude repeats a mistake, the fix usually belongs in CLAUDE.md.

### Plan mode

Claude reads and thinks but can't change anything, then hands you a plan to
approve. This is the cheapest moment to fix a wrong idea.

- Enter with `Shift` + `Tab`, or start with `claude --permission-mode plan`.
- Push back on the plan before approving.
- Skip it for tiny changes. Use it for anything that touches more than one file.

### Checkpoints and git

- **Checkpoints:** Claude saves file state before each of your prompts. `Esc` `Esc` or `/rewind` restores code, conversation, or both. It covers Claude's file edits, not changes made by shell commands (deleted files, installed packages).
- **Git:** permanent history. Claude can commit, branch, write messages and read diffs. Commit after each working milestone; try risky ideas on a branch.

### Managing context

| Command | When to use it |
|---|---|
| `/clear` | Between unrelated tasks or milestones. The most useful habit |
| `/compact` | Mid-task when things get long. Add a focus: `/compact keep the collision bug notes` |
| `/context` | See what's taking up space |
| `/resume` | Reopen an earlier session. Terminal: `claude --continue` or `claude --resume` |
| `/model` | Switch models |
| `/cost` | Token use for this session |

If you've corrected Claude twice on the same thing and it's still wrong, the
context is cluttered. `/clear` and write a better first prompt.

### Letting Claude see the result

- **Paste screenshots** of bugs or mockups.
- **Preview browser** (desktop app): Claude starts a local server, opens the page, clicks, reads the console, takes screenshots. Server settings go in `.claude/launch.json`:

  ```json
  {
    "version": "0.0.1",
    "configurations": [
      { "name": "game", "runtimeExecutable": "python3",
        "runtimeArgs": ["-m", "http.server", "8000"], "port": 8000 }
    ]
  }
  ```

- **Browser over MCP** (terminal): `claude mcp add playwright -- npx @playwright/mcp@latest`.

### Skills and slash commands

A skill is a folder of instructions Claude loads when relevant, or when you
type its name with a slash.

| Command | What it does |
|---|---|
| `/init` | Writes a CLAUDE.md from your code |
| `/code-review` | Reviews your changes for bugs |
| `/simplify` | Cleans up duplication and overgrown code |
| `/security-review` | Checks pending changes for security issues |
| `/loop` | Repeats a prompt on an interval |
| `/schedule` | Runs a cloud agent on a schedule |
| `/fewer-permission-prompts` | Suggests an allowlist from commands you approve often |

Your own skill lives in `.claude/skills/<name>/SKILL.md` (or `~/.claude/skills/`
for every project):

```markdown
---
name: playtest
description: Play the game in the browser and report bugs. Use after any gameplay change.
---
1. Start the "game" server from .claude/launch.json and open it.
2. Play for 30 seconds: move the paddle, break bricks, lose a life.
3. Check the browser console for errors.
4. Take one screenshot.
5. Report: what works, what's broken, what feels off. Don't fix anything yet.
```

### Subagents

A helper with its own context window that returns only a summary. Built-ins
include Explore, Plan and a general-purpose agent. Create your own with
`/agents` or a file in `.claude/agents/`:

```markdown
---
name: physics-reviewer
description: Reviews collision and movement code for edge cases. Use after physics changes.
tools: Read, Grep, Bash
---
You review 2D game physics. Look for tunneling at high speed,
corner hits, floating-point drift and frame-rate dependence.
Report each issue with the file, line, and a test that would catch it.
```

### Hooks

Shell commands that always run at set moments. CLAUDE.md is advice; a hook is
enforced.

| Event | Example use |
|---|---|
| `PostToolUse` | Format or lint files after Claude edits them |
| `PreToolUse` | Block edits to certain files |
| `Stop` | Run tests when Claude finishes |
| `Notification` | Desktop alert when Claude needs input |
| `SessionStart` | Load extra context when a session begins |

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Edit|Write",
        "hooks": [{ "type": "command", "command": "npx prettier --write '*.js'" }]
      }
    ]
  }
}
```

### MCP servers and plugins

MCP servers give Claude new tools (browser, database, issue tracker). Check
status with `/mcp`.

```sh
claude mcp add playwright -- npx @playwright/mcp@latest
claude mcp add --transport http <name> <url>
```

Plugins bundle skills, subagents, hooks and MCP servers. Browse with `/plugin`.

### Permissions and settings

| File | Scope |
|---|---|
| `~/.claude/settings.json` | You, in every project |
| `.claude/settings.json` | This project, shared through git |
| `.claude/settings.local.json` | This project, only you |

```json
{
  "permissions": {
    "allow": ["Bash(node --test:*)", "Bash(git status)", "Bash(git diff:*)"],
    "deny":  ["Read(./.env)"]
  }
}
```

### Parallel work and automation

- **Several sessions at once** in the desktop app, each in its own git worktree.
- **Headless mode:** `claude -p "list TODOs in game.js"` runs once and prints.
- **GitHub:** `/install-github-app`, then mention `@claude` in issues or PRs.
- **Recurring work:** `/loop` in-session, `/schedule` for cloud agents.
- **Artifacts:** the desktop app can publish a page as a private shareable link.

### Cheat sheet

| Command | Purpose |
|---|---|
| `/help` | List commands |
| `/init` | Create CLAUDE.md |
| `/memory` | Edit memory files |
| `/clear` | Fresh conversation |
| `/compact` | Summarize to free space |
| `/context` | Context usage |
| `/rewind` | Restore a checkpoint |
| `/resume` | Reopen a past session |
| `/model` | Change model |
| `/cost` | Session token usage |
| `/agents` | Manage subagents |
| `/mcp` | Manage MCP servers |
| `/plugin` | Plugins |
| `/hooks`, `/permissions`, `/config`, `/doctor` | Terminal-only dialogs |

| Terminal | Purpose |
|---|---|
| `claude` | Interactive session |
| `claude "prompt"` | Start with a first prompt |
| `claude -p "prompt"` | Run once, print, exit |
| `claude --continue` | Continue the most recent session |
| `claude --resume` | Pick a past session |
| `claude --permission-mode plan` | Start in plan mode |

### Practice project: Brick Breaker in 12 steps

1. Create the folder and start `claude`.
2. Write the spec together: "Interview me one question at a time… then write GAME.md with 5–6 playable milestones. Don't write code yet."
3. Set house rules in CLAUDE.md (plain HTML/CSS/JS, no build step, game.js / render.js / input.js split).
4. Plan milestone 1 in plan mode (paddle + bouncing ball).
5. Build it and look at it in the preview browser.
6. Save the working version.
7. `/clear`, plan milestone 2 (bricks and score).
8. Move collision math into `physics.js` with `node --test` tests.
9. Fix bugs with evidence: steps, expected vs actual, screenshot; ask for the cause before the fix.
10. Polish one feature per turn.
11. `/code-review`, then `/simplify`.
12. Share it as a single HTML file.

---

## Part 2 — Rye & Rent game plan

### The game

A turn-based farm economy set in the age of the Varangians. You hold a farm
and owe rent to the prince every season. Rent climbs, so standing still means
going broke. You open new lands, plant crops, and build workshops that turn
cheap goods into valuable ones (wheat → mill → flour → bakery → bread), and
each deeper step costs more. Enemies pay well for your goods, but everything
you sell them makes them stronger. As the farm grows it attracts the
Varangians. Run out of money and you lose.

### One turn (one season)

1. **Event** — harvest luck, a Varangian ship, an enemy demand.
2. **Decide** — buy land, plant, build, set production.
3. **Produce** — fields harvest; buildings convert inputs to outputs.
4. **Trade** — market, enemies, or Varangians.
5. **Pay rent** — if you can't, the game ends.

### Systems (starting formulas, to be tuned)

| System | Rule |
|---|---|
| Rent | `rent = 10 × 1.08^season + 4 × lands` |
| New lands | `land cost = 40 × 1.3^owned` |
| Production chains | `cost = 60 × 1.8^(steps−1) × 1.25^same_owned` (mill 60, bakery 108) |
| Market | Price drops 4% per unit sold, recovers 15% per season |
| Enemy trade | Pays 1.5× market; `strength += value sold`; tolls → raids → burned building at thresholds |
| Varangians | Visit every 4–6 seasons once farm value ≥ 300; buy bread/mead high, sell furs/silver, open river land, sometimes demand tribute |

### Starting numbers

| Good | Tier | Made by | Input | Price |
|---|---|---|---|---:|
| Wheat | 1 | Field | none, 10 per field | 2 |
| Flax | 1 | Field | none, 6 per field | 3 |
| Honey | 1 | Apiary | none, 4 per apiary | 4 |
| Flour | 2 | Mill | 2 wheat | 6 |
| Linen | 2 | Loom | 2 flax | 9 |
| Bread | 3 | Bakery | 1 flour | 14 |
| Mead | 3 | Meadery | 2 honey | 16 |

Start with 100 silver, one land, one field.

### Open questions (with defaults)

| Question | Default |
|---|---|
| Where does it run? | Browser, plain HTML/JS, plus a terminal text version |
| One player or several? | One player; enemies are computer factions |
| How do you win? | Survive 40 seasons; score is final wealth |
| What happens when enemies get strong? | Tolls, then raids that steal goods, then a burned building |
| Varangians: friends or foes? | Both: trade partners who may demand tribute |
| Building upkeep? | Yes, small upkeep per season |

### Tech choices

- The engine is pure: `(state, action) → new state`. No DOM, no console.
- All numbers live in `src/config.js`.
- `node play.js` plays the game as text, so Claude can test it in the terminal.
- `node sim.js` plays hundreds of games with bots to tune balance.
- Tests with `node --test`; no packages.

```
rye-and-rent/
├── CLAUDE.md
├── GAME.md
├── src/
│   ├── config.js      # goods, buildings, recipes, rent
│   ├── engine.js      # newGame(), applyAction(), endTurn()
│   ├── market.js
│   ├── factions.js
│   └── events.js
├── play.js            # terminal version
├── sim.js             # balance simulator
├── web/
│   ├── index.html
│   └── ui.js
└── test/
```

### Claude Code setup for the game

`CLAUDE.md`:

```markdown
# Rye & Rent
Turn-based farm economy game. Design and milestones: @GAME.md

## Architecture
- src/engine.js is pure: (state, action) -> new state. No DOM, no console, no Math.random directly
- Randomness comes from a seeded RNG stored in state, so games can be replayed
- Every number (prices, costs, rent, odds) lives in src/config.js. Never hard-code numbers in logic
- play.js (terminal) and web/ui.js (browser) only display state and send actions

## Workflow
- Run `node --test` after any change in src/
- After a gameplay change, play 10 seasons with `node play.js --auto` and check the log
- One milestone at a time. When done, tick it in GAME.md and tell me how to try it
- Keep functions short and named after game terms (payRent, openLand, runMill)
```

`.claude/settings.json` (no git):

```json
{
  "permissions": {
    "allow": ["Bash(node --test:*)", "Bash(node play.js:*)", "Bash(node sim.js:*)"]
  },
  "hooks": {
    "Stop": [{ "hooks": [{ "type": "command", "command": "node --test 2>&1 | tail -5" }] }]
  }
}
```

`.claude/skills/balance/SKILL.md`:

```markdown
---
name: balance
description: Run the balance simulator and suggest config changes. Use when asked about difficulty, balance, or economy numbers.
---
1. Run `node sim.js --games 500` for every bot strategy.
2. Report per strategy: median season of bankruptcy, % surviving 40 seasons, final wealth.
3. Target: the "hold" bot goes broke by season 12; the best bot survives 40 seasons in 40–70% of games.
4. Suggest at most 3 changes to src/config.js, with the reason for each. Don't apply them until I agree.
```

`.claude/agents/economy-reviewer.md`:

```markdown
---
name: economy-reviewer
description: Looks for exploits and dead ends in the game economy. Use after adding a system.
tools: Read, Grep, Bash
---
You review economy games for exploits. Look for infinite money loops,
buildings that never pay back, dominant strategies, and choices that
are always wrong. Use node sim.js to prove each finding. Report each
with the config values involved and a proposed fix.
```

### 12 milestones

Each runs the same loop: plan mode → build → check → back up → `/clear`.

1. **Create the project.** `mkdir rye-and-rent && cd rye-and-rent && claude`
2. **Design interview → GAME.md.**
   > I want to make a turn-based farm economy game. Idea: I hold a farm and pay rent that rises every season. Growing the farm opens new lands and attracts the Varangians. I can trade with enemies for money, but the goods make them stronger. Resources can be processed into more valuable goods (wheat → mill → flour → bakery → bread), and each deeper step costs more. If I run out of money I lose.
   >
   > Interview me one short question at a time about the open points: platform, win condition, enemies, Varangians, upkeep, and anything else you find unclear. Offer a default for each question. Then write GAME.md with: the rules, one turn step by step, every system with a formula, a table of starting numbers, and a milestone checklist in this order: engine core, lands and wheat, production chains, browser version, market prices, enemies, Varangians and events, balance simulator, polish. Don't write code.
3. **House rules and tooling.** Create the four setup files above. Done when `/balance` appears under `/`.
4. **Engine core: seasons, rent, bankruptcy.**
   > Read @GAME.md. Plan only the engine core: src/config.js, a seeded RNG, and src/engine.js with newGame(seed), endTurn(state) that charges rising rent, and a game-over state when money can't cover rent. Add play.js that shows money, season and next rent in the terminal, with an --auto flag that just ends turns. Add tests for the rent formula and bankruptcy.
5. **Lands, fields and selling wheat.**
   > Read @GAME.md and the current code. Plan the lands and wheat milestone: actions openLand, buildField, and sell(good, amount). Fields produce wheat each season. Land cost and rent both grow with lands owned, as in GAME.md. Show the actions as a numbered menu in play.js.
6. **Production chains.**
   > Plan the production chains milestone: chains driven entirely by data in config.js. Each building has a recipe (inputs → outputs per season), a tier, a base cost and upkeep. Cost scales with tier and with how many of that building I already own, using the formula in GAME.md. Start with mill (wheat → flour) and bakery (flour → bread). Adding a new chain must need only a config change. Prove that with a test that adds a fake chain.
7. **First browser version.**
   > Plan the browser version: web/index.html and web/ui.js that use the same engine. Show money, season, next rent (in red when I can't afford it), my lands and buildings, stock of each good, and buttons for every action. Keep it plain but readable. No logic in ui.js; it only calls engine functions. Tell me the command to serve it locally.
8. **Market with moving prices.**
   > Plan the market milestone: src/market.js. Selling lowers a good's price per unit sold; prices recover toward base each season, as in GAME.md. Show the price before and after a sale in both interfaces. Add tests for price drop and recovery.
9. **Enemies and the trade trade-off.**
   > Plan the enemies milestone: src/factions.js with one enemy faction. I can sell goods to them at a better price than the market. Their strength grows by the value of goods I sell them. At the thresholds in GAME.md they charge tolls, raid my stock, then burn a building. Show their strength as a warning meter, and warn me before a trade crosses a threshold.

   Then: "Use the economy-reviewer agent to check enemy trade for exploits."
10. **Varangians and seasonal events.**
    > Plan the events milestone: src/events.js with a data-driven list of seasonal events (good harvest, drought, fair, prince raises rent). Then the Varangians: they arrive once farm value passes the threshold in GAME.md, offer to buy bread and mead at high prices, sell furs and silver, can open a new river land, and sometimes demand tribute. Refusing tribute has a risk that depends on my defences. Each visit is a choice screen with 2–3 options.
11. **Balance lab.**
    > Plan the balance simulator: sim.js that plays many seeded games with bot strategies: "hold" (does nothing), "farmer" (only fields), "miller" (builds up to flour), "baker" (full chain), "traitor" (sells mostly to enemies). Flags: --games N and --bot NAME. Output a table per bot: median bankruptcy season, % surviving 40 seasons, median final wealth.

    Then run `/balance` until it feels right.
12. **Polish, save, review, share.** One prompt at a time: save/load in localStorage; money and rent chart; short tutorial; game-over screen. Then `/code-review` and `/simplify`. Publish, e.g. as an HTML game on itch.io.

### Session rhythm

| Step | What you do |
|---|---|
| 1. Fresh start | `/clear`. GAME.md and CLAUDE.md carry the memory |
| 2. Plan | `Shift`+`Tab` to plan mode, paste the milestone prompt, correct the plan |
| 3. Build | Approve. `Esc` the moment something looks wrong |
| 4. Check | Play in `node play.js` or the browser. Tests run by hook |
| 5. Fix | Bug with steps and a screenshot. `Esc` `Esc` to rewind a bad fix |
| 6. Back up | "Copy the project into ../rye-and-rent-backups/step-NN, skipping node_modules." |

Use the strongest model (`/model`) for production chains, enemies, Varangians
and balance.

### Pitfalls

- Numbers scattered in code. Keep everything in `config.js`.
- Balancing by feel alone. Run the simulator after every numbers change.
- Infinite money loops. The reviewer agent looks for these.
- Adding systems before the core is fun. Tune rent first.
- Logic in the UI. Rules only in `ui.js` are invisible to tests and the simulator.

---

## Part 3 — Rewind recovery and working without git

### After an accidental `Esc` `Esc`

- **Only the rewind list opened:** nothing changed. Press `Esc` to close it.
- **There was text in the input box:** it was cleared. Press `↑` to try to bring it back.
- **A restore was confirmed:**
  - Conversation: try `/resume`, or `claude --resume` in the terminal, and look for the session from before the rewind. Later messages may not survive a restore in every version.
  - Code: there is no redo. Ask Claude to redo the lost step, e.g. "Redo the production chains milestone from GAME.md."
- When rewinding on purpose and you only want the chat back, choose **conversation only** so files aren't touched.

### Working without git

- Skip `git init` and commit steps.
- After each milestone: "Copy the project into ../rye-and-rent-backups/step-NN". To recover, copy the last good backup back.
- Rewind only covers files Claude edited, not changes from shell commands, so backups are the main safety net.

---

## Part 4 — Three.js browser UI prompt

Before pasting:

1. Back up: "Copy the project into ../rye-and-rent-backups/before-3d".
2. Switch to plan mode (`Shift`+`Tab`) and the strongest model (`/model`).
3. Optional, so Claude can take its own screenshots:
   `claude mcp add playwright -- npx @playwright/mcp@latest`

```text
Build a browser version of this game with a 3D view in Three.js. The terminal game is finished and must keep working exactly as it does now.

BEFORE CODING
- Read GAME.md, CLAUDE.md, everything in src/, and play.js. List every piece of state the player needs to see and every action they can take. Every action play.js offers must also be available in the browser.
- Check whether src/ uses CommonJS (require / module.exports). If it does, convert it to ES modules so the same files load in both Node and the browser. Update play.js, sim.js and the tests to match. `node --test` must still pass and `node play.js --auto` must still run.
- Show me the plan and wait for my OK. After that, build everything without stopping unless you're blocked.

RULES
- Don't change any game rules or numbers. The UI only reads state and calls engine functions. No game logic in UI files.
- No build step and no npm packages. Load Three.js 0.160.0 from jsDelivr with an import map (three + three/addons/ for OrbitControls). Pin the version.
- Files: web/index.html, web/style.css, web/main.js (wiring), web/scene.js (3D), web/hud.js (HTML panels). Split any file that grows past about 300 lines.
- Build all 3D from Three.js primitives only, with no external models or textures. Use a low-poly, flat-shaded, warm medieval style.
- Build the scene from the data in config.js. A building type with no custom model gets a generic hut with a label, so new config entries show up automatically.

3D SCENE: A DIORAMA OF THE FARM
- A board of land tiles. Owned tiles are grass. Locked tiles are darker and show their price on hover. Clicking a locked tile offers to buy it.
- Everything on a tile is visible: wheat fields (instanced stalks that grow during the season and turn gold at harvest), the mill with turning sails, the bakery with chimney smoke, and a model for each other building in config.
- A river along one edge. When the Varangians visit, a longship sails in and docks, then leaves after the visit.
- An enemy camp on the far edge. Tents, banners and fires grow with enemy strength, and it glows red near a threshold.
- The season shows through light and color: spring green, summer bright, autumn gold, winter snow.
- Camera: OrbitControls with limits (it can't go under the ground or too far away) and smooth damping. Click a tile to select it and highlight it.
- End-of-season animations: floating "+10 wheat" numbers above the buildings that produced, and coins flying away when rent is paid.

HUD: HTML OVER THE CANVAS, NO 3D TEXT
- Top bar: silver, season and year, next rent (red when I can't afford it), and an enemy strength meter.
- Selected-tile panel: what's on the tile, what it produces each season, and build options with costs. When I can't afford an option, it's disabled and the panel shows why.
- Stock and market panel: every good with amount and current price, sell buttons (1, 10, all), and a "sell to enemy" option that warns me before a sale crosses a strength threshold.
- Events and Varangian visits appear as choice cards with the options the engine provides.
- A log of the last few seasons.
- A big "End season" button. Keys: Space ends the season, Esc closes panels.
- A game-over screen with the reason, key stats, and a "New game" button.
- Save and continue through localStorage, if the engine state can be serialized.

QUALITY
- Works at 1280x720 and at phone width. On narrow screens the panels become a bottom sheet.
- Smooth on a laptop: use InstancedMesh for repeated objects, and update only what changed each turn instead of rebuilding the scene.
- Handle window resize, and cap devicePixelRatio at 2.
- No errors in the browser console.

ORDER OF WORK (run `node --test` after each step)
1. Convert modules (if needed) and confirm the tests and play.js still work.
2. HUD with an empty canvas. The game must be fully playable in the browser at this point.
3. The 3D board, tiles, and click selection.
4. Buildings and fields.
5. River and Varangian ship, enemy camp, seasons.
6. Animations and polish.
If a browser tool (Playwright MCP) is available, open the page after steps 2, 4 and 6, take a screenshot, check the console, and fix what you see.

WHEN DONE
- Add a "Browser version" section to CLAUDE.md explaining how to run it.
- Tell me the exact command to start a local server from the project root (python3 -m http.server 8000) and the URL to open.
- Give me a short list of things to test by hand.
```

Why it's shaped this way:

- One prompt, but staged: a plan first, then six checked steps.
- The HUD comes before the 3D, so the game is playable in the browser even if the 3D needs work.
- The module check matters: browsers can't load Node's `require`, so without it the browser version fails on the first line.

After it's done, expect a few follow-up fixes. Send a screenshot and one
sentence per problem.
