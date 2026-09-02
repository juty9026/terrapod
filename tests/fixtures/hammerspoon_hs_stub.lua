-- A recording stand-in for the `hs` global that dot_hammerspoon/init.lua
-- expects Hammerspoon to provide.
--
-- Loading this file defines global `hs` and returns a harness table. Only the
-- API init.lua actually calls is implemented; anything else is a hard error so
-- a new hs call in init.lua fails the test instead of passing by accident.
--
-- Everything init.lua does through `hs` lands in `harness.trace`, one line per
-- call, in order. The test driver reads that trace; nothing here asserts.
--
-- Timers never fire on their own. `hs.timer.doAfter` queues the callback and
-- `harness.flushTimers()` runs the queue in delay order, so a scenario decides
-- exactly when Hammerspoon's clock advances.

local harness = {
  trace = {},
  timers = {},
  hotkeys = {},
  modals = {},
  appWatchers = {},
  windowSubscriptions = {},
  apps = {},
  frontmost = nil,
  currentSourceID = "com.apple.inputmethod.Korean.2SetKorean",
  -- hs.keycodes.currentSourceID(id) and hs.keycodes.setLayout(name) report
  -- these results. When one succeeds the current source becomes its target,
  -- the way the real calls change the input source.
  setSourceSucceeds = true,
  setLayoutSucceeds = true,
}

local function record(line)
  table.insert(harness.trace, line)
end

local function formatMods(mods)
  if #mods == 0 then
    return "none"
  end

  local copy = {}
  for _, mod in ipairs(mods) do
    table.insert(copy, mod)
  end
  table.sort(copy)
  return table.concat(copy, "+")
end

local function chordName(mods, key)
  return formatMods(mods) .. " " .. key
end

-- hs.hotkey.bind and modal:bind accept an optional message string before the
-- pressed function. init.lua never passes one, but the stub accepts either
-- shape so a future edit does not silently bind nothing.
local function splitHotkeyArgs(...)
  local first, second = ...
  if type(first) == "function" then
    return first, second
  end
  return second, (select(3, ...))
end

-- Application objects --------------------------------------------------------

local appMethods = {}
appMethods.__index = appMethods

function appMethods:bundleID()
  return self.id
end

function appMethods:isHidden()
  return self.hidden
end

function appMethods:unhide()
  record("app.unhide " .. self.id)
  self.hidden = false
end

function appMethods:activate()
  record("app.activate " .. self.id)
  harness.frontmost = self
  return true
end

-- Registers a running application the scenario can return from
-- hs.application.get and hand to activateApp.
function harness.addApp(bundleID, options)
  options = options or {}
  local app = setmetatable({ id = bundleID, hidden = options.hidden or false }, appMethods)
  harness.apps[bundleID] = app
  return app
end

-- Replays what Hammerspoon does when an app comes to the front: the frontmost
-- application changes and every hs.application.watcher hears `activated`.
function harness.activateApp(app)
  harness.frontmost = app
  for _, callback in ipairs(harness.appWatchers) do
    callback(app and app.id or nil, hs.application.watcher.activated, app)
  end
end

-- Fires every subscriber init.lua registered for the given window event.
function harness.focusWindow(event)
  for _, subscription in ipairs(harness.windowSubscriptions) do
    if subscription.event == event then
      subscription.callback()
    end
  end
end

-- Key events ----------------------------------------------------------------

-- An entered modal's binding wins over a global hotkey for the same chord,
-- which is how Hammerspoon resolves the two while a modal is active.
function harness.press(key, mods)
  mods = mods or {}
  local chord = chordName(mods, key)

  for _, modal in ipairs(harness.modals) do
    if modal.entered and modal.bindings[chord] then
      record("press " .. chord .. " (modal)")
      modal.bindings[chord].pressed()
      return
    end
  end

  local hotkey = harness.hotkeys[chord]
  if not hotkey then
    error("no hotkey bound for " .. chord)
  end

  record("press " .. chord)
  hotkey.pressed()
end

function harness.release(key, mods)
  mods = mods or {}
  local chord = chordName(mods, key)
  local hotkey = harness.hotkeys[chord]
  if not hotkey then
    error("no hotkey bound for " .. chord)
  end

  record("release " .. chord)
  if hotkey.released then
    hotkey.released()
  end
end

-- Timers --------------------------------------------------------------------

local timerMethods = {}
timerMethods.__index = timerMethods

function timerMethods:stop()
  if not self.stopped and not self.fired then
    record(string.format("timer.stop %.2f", self.delay))
  end
  self.stopped = true
end

function harness.pendingTimers()
  local count = 0
  for _, timer in ipairs(harness.timers) do
    if not timer.stopped and not timer.fired then
      count = count + 1
    end
  end
  return count
end

-- Runs queued timers in delay order until none are left, including timers a
-- callback queues while the flush is running. A callback that stops a later
-- timer keeps that timer from firing, as it would under a real clock. With a
-- limit, at most that many timers fire so a scenario can act between two.
function harness.flushTimers(limit)
  local fired = 0
  while limit == nil or fired < limit do
    local due = nil
    for _, timer in ipairs(harness.timers) do
      if not timer.stopped and not timer.fired and (due == nil or timer.delay < due.delay) then
        due = timer
      end
    end

    if due == nil then
      return
    end

    due.fired = true
    fired = fired + 1
    record(string.format("timer.fire %.2f", due.delay))
    due.callback()
  end
end

-- Output --------------------------------------------------------------------

function harness.printTrace()
  for _, line in ipairs(harness.trace) do
    print(line)
  end
end

-- hs ------------------------------------------------------------------------

local function unsupported(name)
  return setmetatable({}, {
    __index = function(_, key)
      error("hs stub does not implement " .. name .. "." .. tostring(key), 2)
    end,
  })
end

hs = unsupported("hs")

hs.application = unsupported("hs.application")

function hs.application.get(bundleID)
  return harness.apps[bundleID]
end

function hs.application.launchOrFocusByBundleID(bundleID)
  record("application.launchOrFocusByBundleID " .. bundleID)
  return harness.apps[bundleID] ~= nil
end

function hs.application.frontmostApplication()
  return harness.frontmost
end

hs.application.watcher = unsupported("hs.application.watcher")
hs.application.watcher.activated = "activated"

function hs.application.watcher.new(callback)
  local watcher = { callback = callback }
  function watcher:start()
    record("application.watcher.start")
    table.insert(harness.appWatchers, self.callback)
    return self
  end
  return watcher
end

hs.alert = unsupported("hs.alert")

function hs.alert.show(message)
  record("alert.show " .. (message:gsub("\n", "\\n")))
  return "alert-" .. #harness.trace
end

function hs.alert.closeSpecific(id)
  record("alert.closeSpecific " .. tostring(id))
end

hs.keycodes = unsupported("hs.keycodes")

function hs.keycodes.currentSourceID(sourceID)
  if sourceID == nil then
    return harness.currentSourceID
  end

  record("keycodes.currentSourceID set " .. sourceID)
  if harness.setSourceSucceeds then
    harness.currentSourceID = sourceID
  end
  return harness.setSourceSucceeds
end

function hs.keycodes.setLayout(name)
  record("keycodes.setLayout " .. name)
  if harness.setLayoutSucceeds then
    harness.currentSourceID = "com.apple.keylayout." .. name
  end
  return harness.setLayoutSucceeds
end

hs.timer = unsupported("hs.timer")

function hs.timer.doAfter(delay, callback)
  local timer = setmetatable({ delay = delay, callback = callback, stopped = false, fired = false }, timerMethods)
  record(string.format("timer.doAfter %.2f", delay))
  table.insert(harness.timers, timer)
  return timer
end

hs.hotkey = unsupported("hs.hotkey")

function hs.hotkey.bind(mods, key, ...)
  local pressed, released = splitHotkeyArgs(...)
  local chord = chordName(mods, key)
  record("hotkey.bind " .. chord)
  harness.hotkeys[chord] = { pressed = pressed, released = released }
end

hs.hotkey.modal = unsupported("hs.hotkey.modal")

local modalMethods = {}
modalMethods.__index = modalMethods

function modalMethods:bind(mods, key, ...)
  local pressed, released = splitHotkeyArgs(...)
  local chord = chordName(mods, key)
  record("modal.bind " .. chord)
  self.bindings[chord] = { pressed = pressed, released = released }
  return self
end

function modalMethods:enter()
  record("modal.enter")
  self.entered = true
  return self
end

function modalMethods:exit()
  record("modal.exit")
  self.entered = false
  return self
end

function hs.hotkey.modal.new()
  local modal = setmetatable({ bindings = {}, entered = false }, modalMethods)
  table.insert(harness.modals, modal)
  return modal
end

hs.screen = unsupported("hs.screen")

function hs.screen.mainScreen()
  return "main-screen"
end

function hs.reload()
  record("reload")
end

hs.window = unsupported("hs.window")
hs.window.filter = unsupported("hs.window.filter")
hs.window.filter.windowFocused = "windowFocused"

function hs.window.filter.new()
  local filter = {}

  function filter:setAppFilter(appName)
    record("window.filter.setAppFilter " .. appName)
    return self
  end

  function filter:subscribe(event, callback)
    record("window.filter.subscribe " .. event)
    table.insert(harness.windowSubscriptions, { event = event, callback = callback })
    return self
  end

  return filter
end

return harness
