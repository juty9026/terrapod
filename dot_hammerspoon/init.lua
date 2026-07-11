local hyper = { "ctrl", "alt", "cmd", "shift" }
local leaderKey = "f18"
local englishSourceID = "com.apple.keylayout.ABC"
local ghosttyAppName = "Ghostty"
local ghosttyBundleID = "com.mitchellh.ghostty"
local ghosttyEnglishRetryDelays = { 0.00, 0.08, 0.18, 0.35 }
local helpToastStyle = {
  atScreenEdge = 0,
  fadeInDuration = 0.10,
  fadeOutDuration = 0.10,
  fillColor = { white = 0.10, alpha = 0.92 },
  radius = 16,
  strokeWidth = 0,
  textColor = { white = 1, alpha = 1 },
  textSize = 16,
  padding = 8,
}

local appBindings = {
  { key = "t", label = ghosttyAppName, bundleID = ghosttyBundleID },
  { key = "s", label = "Slack", bundleID = "com.tinyspeck.slackmacgap" },
  { key = "d", label = "Discord", bundleID = "com.hnc.Discord" },
  { key = "n", label = "Notion", bundleID = "notion.id" },
  { key = "b", label = "Google Chrome", bundleID = "com.google.Chrome" },
  { key = "1", label = "ChatGPT", bundleID = "com.openai.codex" },
  { key = "2", label = "Claude Desktop", bundleID = "com.anthropic.claudefordesktop" },
  { key = "3", label = "Antigravity 2.0", bundleID = "com.google.antigravity" },
  { key = "i", label = "Antigravity IDE", bundleID = "com.google.antigravity-ide" },
  { key = "o", label = "Obsidian", bundleID = "md.obsidian" },
  { key = "p", label = "Postman", bundleID = "com.postmanlabs.mac" },
  { key = "f", label = "Figma", bundleID = "com.figma.Desktop" },
}
local helpToastID
local ghosttyRetryTimers = {}

local function launchOrFocus(appConfig)
  local app = hs.application.get(appConfig.bundleID)

  if app then
    if app:isHidden() then
      app:unhide()
    end

    app:activate()
    return
  end

  local launched = hs.application.launchOrFocusByBundleID(appConfig.bundleID)

  if not launched then
    hs.alert.show("Could not launch " .. appConfig.label)
  end
end

local function requestInputSource()
  return hs.keycodes.currentSourceID(englishSourceID) or hs.keycodes.setLayout("ABC")
end

local function cancelGhosttyInputRetries()
  for _, timer in ipairs(ghosttyRetryTimers) do
    timer:stop()
  end

  ghosttyRetryTimers = {}
end

local function isGhosttyFrontmost()
  local app = hs.application.frontmostApplication()

  return app and app:bundleID() == ghosttyBundleID
end

local function ensureGhosttyEnglish()
  cancelGhosttyInputRetries()

  for index, delay in ipairs(ghosttyEnglishRetryDelays) do
    local timer = hs.timer.doAfter(delay, function()
      if not isGhosttyFrontmost() then
        cancelGhosttyInputRetries()
        return
      end

      local currentSourceID = hs.keycodes.currentSourceID()

      if currentSourceID ~= englishSourceID then
        requestInputSource()
      end

      if index == #ghosttyEnglishRetryDelays then
        cancelGhosttyInputRetries()
      end
    end)

    table.insert(ghosttyRetryTimers, timer)
  end
end

local helpLines = {
  "Hyper key launcher",
  "",
  "Caps Lock (F18) or ctrl+alt+cmd+shift",
  "",
}

for _, appConfig in ipairs(appBindings) do
  table.insert(helpLines, string.upper(appConfig.key) .. " : " .. appConfig.label)
end

table.insert(helpLines, "")
table.insert(helpLines, "R : Reload Hammerspoon")
table.insert(helpLines, "/ : Show this help")
table.insert(helpLines, "Ghostty focus : Switch to ABC")

local launcherModal = hs.hotkey.modal.new()
local leaderState = {
  active = false,
}

local function exitLeader()
  if not leaderState.active then
    return
  end

  leaderState.active = false
  launcherModal:exit()
end

for _, appConfig in ipairs(appBindings) do
  hs.hotkey.bind(hyper, appConfig.key, function()
    launchOrFocus(appConfig)
  end)

  launcherModal:bind({}, appConfig.key, function()
    launchOrFocus(appConfig)
    exitLeader()
  end)
end

local function showHelp()
  if helpToastID then
    hs.alert.closeSpecific(helpToastID, 0)
  end

  helpToastID = hs.alert.show(table.concat(helpLines, "\n"), helpToastStyle, hs.screen.mainScreen(), 5)
end

hs.hotkey.bind(hyper, "/", showHelp)
hs.hotkey.bind(hyper, "r", function()
  hs.reload()
end)

launcherModal:bind({}, "/", function()
  showHelp()
  exitLeader()
end)

launcherModal:bind({}, "r", function()
  hs.reload()
  exitLeader()
end)

hs.hotkey.bind({}, leaderKey, function()
  if leaderState.active then
    return
  end

  leaderState.active = true
  launcherModal:enter()
end, function()
  exitLeader()
end)

local ghosttyWatcher = hs.application.watcher.new(function(_, eventType, appObject)
  if eventType ~= hs.application.watcher.activated then
    return
  end

  if appObject and appObject:bundleID() == ghosttyBundleID then
    ensureGhosttyEnglish()
    return
  end

  cancelGhosttyInputRetries()
end)

ghosttyWatcher:start()

local ghosttyWindowFilter = hs.window.filter.new(false):setAppFilter(ghosttyAppName)

ghosttyWindowFilter:subscribe(hs.window.filter.windowFocused, function()
  ensureGhosttyEnglish()
end)

hs.alert.show("Hammerspoon config loaded")
