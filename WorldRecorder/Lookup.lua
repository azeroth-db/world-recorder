-- WOW-41: local reference state. No recorder save, HTTP, account, or uploader access.
local _, ns = ...
local L = ns.LookupText
local lookup = { query = "", filter = "all", history = {}, results = {}, offset = 0, generation = 0 }
ns.Lookup = lookup
lookup.metrics = { initializationMilliseconds = debugprofilestop() - (ns.LookupStartedAt or debugprofilestop()), searchCount = 0, searchMilliseconds = 0, detailMilliseconds = 0, requests = 0 }

local function plain(value)
  if issecretvalue and issecretvalue(value) then return nil end
  return value
end
ns.LookupPlain = plain
local function text(value)
  value = plain(value)
  if type(value) == "string" and value ~= "" then return value end
end
local function number(value)
  value = plain(value)
  if type(value) == "number" then return value end
end
local function call(fn, ...)
  if type(fn) ~= "function" then return nil end
  local ok, value = pcall(fn, ...)
  if ok then return plain(value) end
end
local function clock() return debugprofilestop and debugprofilestop() or 0 end
local function lower(value)
  -- Generated from the same Unicode casefold database as record.searchName.
  return (value:gsub("[%z\1-\127\194-\244][\128-\191]*", function(character)
    return ns.LookupCasefold and ns.LookupCasefold[character] or character
  end))
end
local function idFrom(value)
  local id = tonumber(value)
  if id and id > 0 and id == math.floor(id) then return id end
end

-- Keep a complete native item link (including bonuses), not just its base ID.
function lookup.Parse(value)
  value = text(value)
  if not value then return end
  local kind, id = value:match("|H(item):(%d+)[^|]*|h")
  if not kind then kind, id = value:match("|H(quest):(%d+)[^|]*|h") end
  if not kind then kind, id = value:match("^%s*(item):(%d+)[%d:%-]*%s*$") end
  if not kind then kind, id = value:match("^%s*(quest):(%d+)[%d:%-]*%s*$") end
  id = idFrom(id)
  if kind and id then
    return { key = kind .. ":" .. id, kind = kind, id = id, link = value }
  end
end

local function notify()
  if lookup.OnChange then lookup.OnChange() end
end
local function package()
  lookup.packageWarning = nil
  local data = ns.LookupData
  if not data or not ns.LookupManifest then return nil, "missingPackage" end
  local manifest = ns.LookupManifest
  for _, key in ipairs({ "schema", "ruleset", "build", "revision", "sourceSHA256", "questRevision", "sourceDigest", "unicodeVersion" }) do
    if data[key] == nil or data[key] ~= manifest[key] then return nil, "missingPackage" end
  end
  if type(ns.LookupCasefold) ~= "table" or ns.LookupCasefoldVersion ~= data.unicodeVersion then return nil, "missingPackage" end
  if data.schema ~= 1 or data.ruleset ~= "forever" or data.locale ~= ns.LookupLocale then
    return nil, "missingPackage"
  end
  local _, build = GetBuildInfo()
  local expected = tostring(data.build):match("(%d+)$")
  lookup.packageWarning = (not expected or tostring(build) ~= expected) and "wrongBuild" or nil
  return data
end
function lookup.Data()
  local data, reason = package()
  lookup.packageReason = reason
  return data
end
function lookup.Record(key)
  local data = lookup.Data()
  return data and data.records[key]
end

function lookup.Search(query, filter)
  local started = clock()
  lookup.generation = lookup.generation + 1
  lookup.query, lookup.filter = query or lookup.query, filter or lookup.filter
  lookup.selected, lookup.detail, lookup.native, lookup.pending = nil, nil, nil, nil
  lookup.history, lookup.results, lookup.offset = {}, {}, 0
  local parsed = lookup.Parse(lookup.query)
  if parsed and (lookup.filter == "all" or lookup.filter == parsed.kind) then
    lookup.results[1] = parsed
  else
    local data = lookup.Data()
    local needle = lower(lookup.query):match("^%s*(.-)%s*$")
    if data and needle ~= "" then
      for _, key in ipairs(data.search) do
        local row = data.records[key]
        if row and (lookup.filter == "all" or row.kind == lookup.filter) and
          (row.searchName or lower(row.name)):find(needle, 1, true) then
          lookup.results[#lookup.results + 1] = row
        end
      end
    end
  end
  lookup.metrics.searchCount = lookup.metrics.searchCount + 1
  lookup.metrics.searchMilliseconds = clock() - started
  notify()
end

local function native(selection)
  local result = { fields = {} }
  if selection.kind == "item" then
    if not (C_Item and C_Item.GetItemInfo) then return result end
    local ok, name, link, quality, level, required, kind, subtype, stack, slot, icon =
      pcall(C_Item.GetItemInfo, selection.link or selection.id)
    if not ok then return result end
    result.name, result.link, result.icon = text(name), text(link), number(icon)
    result.quality = number(quality)
    for _, pair in ipairs({ { "itemLevel", number(level) }, { "requiredLevel", number(required) },
      { "itemType", text(subtype) or text(kind) } }) do
      if pair[2] ~= nil then result.fields[#result.fields + 1] = { label = pair[1], value = pair[2] } end
    end
    slot = text(slot)
    if slot and text(_G[slot]) then
      result.fields[#result.fields + 1] = { label = "slot", value = text(_G[slot]) }
    end
  elseif selection.kind == "quest" then
    result.name = text(call(C_QuestLog and C_QuestLog.GetTitleForQuestID, selection.id))
    local objectives = call(C_QuestLog and C_QuestLog.GetQuestObjectives, selection.id)
    result.objectivesUnavailable = type(objectives) ~= "table"
    if type(objectives) == "table" then
      for _, objective in ipairs(objectives) do
        objective = plain(objective)
        if type(objective) == "table" then
          local value = text(objective.text)
          if value then result.fields[#result.fields + 1] = { label = "objective", value = value } end
        end
      end
    end
    -- Reward APIs here are the recorder's already-used dialog APIs. Never read a
    -- different quest's reward pane or infer unseen rewards from its log title.
    result.relations = {}
    result.rewardsUnavailable = true
    if QuestFrame and QuestFrame:IsShown() and number(call(GetQuestID)) == selection.id then
      result.rewardsUnavailable = false
      for _, kind in ipairs({ "reward", "choice" }) do
        local count = number(call(kind == "reward" and GetNumQuestRewards or GetNumQuestChoices))
        if count then
          for index = 1, count do
            local link = text(call(GetQuestItemLink, kind, index))
            local target = link and lookup.Parse(link)
            local ok, name, icon, amount = pcall(GetQuestItemInfo, kind, index)
            name, amount = ok and text(name), ok and number(amount)
            if target or name then
              result.relations[#result.relations + 1] = { type = "rewards", target = target and target.key,
                link = link, name = name, native = true, quantity = amount, choice = kind == "choice" }
            else result.rewardsUnavailable = true end
          end
        else result.rewardsUnavailable = true end
      end
    end
  end
  result.complete = result.name ~= nil and not result.objectivesUnavailable
  return result
end

local function request()
  local selected = lookup.selected
  if not selected or (selected.kind ~= "item" and selected.kind ~= "quest") then return end
  lookup.native = native(selected)
  if lookup.native.complete then
    lookup.status, lookup.pending = "ready", nil
    notify()
    return
  end
  local fn = selected.kind == "item" and C_Item and C_Item.RequestLoadItemDataByID or
    selected.kind == "quest" and C_QuestLog and C_QuestLog.RequestLoadQuestByID
  if not fn then lookup.status = "unavailable"; notify(); return end
  -- Events have identity but no request token. Never assign the event's identity to selection.
  lookup.pending = { kind = selected.kind, id = selected.id, generation = lookup.generation }
  lookup.status = "waiting"
  lookup.metrics.requests = lookup.metrics.requests + 1
  local ok = pcall(fn, selected.id)
  if not ok then lookup.status, lookup.pending = "failed", nil end
  notify()
end
function lookup.Retry()
  lookup.generation = lookup.generation + 1
  request()
end

function lookup.Select(record, remember)
  if not record then return end
  local started = clock()
  if remember ~= false then
    lookup.history[#lookup.history + 1] = {
      selected = lookup.selected, offset = lookup.offset, query = lookup.query, filter = lookup.filter, detailScroll = lookup.detailScroll,
    }
  end
  lookup.generation = lookup.generation + 1
  lookup.selected = { key = record.key, kind = record.kind, id = record.id, link = record.link }
  lookup.offset, lookup.pending, lookup.native, lookup.detail, lookup.status = 0, nil, nil, nil, nil
  lookup.detailScroll = 0
  local published = lookup.Record(record.key)
  if published and type(published.detail) == "function" then lookup.detail = published.detail() end
  if record.kind == "item" or record.kind == "quest" then request() else notify() end
  lookup.metrics.detailMilliseconds = clock() - started
end
function lookup.Back()
  local prior = table.remove(lookup.history)
  if not prior then return end
  lookup.query, lookup.filter = prior.query, prior.filter
  if prior.selected then
    lookup.Select(prior.selected, false)
  else
    lookup.generation = lookup.generation + 1
    lookup.selected, lookup.pending, lookup.native, lookup.detail, lookup.status = nil, nil, nil, nil, nil
  end
  lookup.offset, lookup.detailScroll = prior.offset, prior.detailScroll or 0
  notify()
end
function lookup.CancelPending()
  lookup.generation = lookup.generation + 1
  lookup.pending = nil
end
function lookup.Refresh()
  if lookup.selected then lookup.Select(lookup.selected, false) else notify() end
end
function lookup.WebsiteRoute()
  local record = lookup.selected and lookup.Record(lookup.selected.key)
  local route = record and text(record.route)
  -- The producer owns exact canonical routes. Never derive one from a displayed name or native ID.
  if route and route:match("^https://wowdb%.wiki/") and not route:find("[\r\n]") then return route end
end

local events = CreateFrame("Frame")
events:RegisterEvent("ITEM_DATA_LOAD_RESULT")
events:RegisterEvent("QUEST_DATA_LOAD_RESULT")
events:SetScript("OnEvent", function(_, event, id, success)
  id, success = number(id), plain(success)
  local pending, selected = lookup.pending, lookup.selected
  local kind = event == "ITEM_DATA_LOAD_RESULT" and "item" or "quest"
  if not id or not selected or not pending or pending.generation ~= lookup.generation or
    selected.kind ~= kind or selected.id ~= id or pending.kind ~= kind or pending.id ~= id then return end
  lookup.native = native(selected)
  -- Keep the current identity subscribed after a failed/incomplete event: native events
  -- carry no attempt token, and a later answer to an explicit Retry may still arrive.
  if lookup.native.complete then lookup.pending = nil end
  -- An older failed response must not erase newer cached facts for this same identity.
  lookup.status = lookup.native.complete and "ready" or (success == false and "failed" or "unavailable")
  notify()
end)
