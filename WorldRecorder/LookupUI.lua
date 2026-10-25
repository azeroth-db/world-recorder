-- Native frames are created on first use. Search never performs a game-service census.
local ADDON, ns = ...
local lookup, L, site = ns.Lookup, ns.LookupText, ns.LookupWebsite
local window, search, message, scroller, content, copy, back, retry, website, copyButton
local resultRows, detailRows, buttons = {}, {}, {}
local renderedKey, resultChoice, relationChoice, syncing = nil, 1, 1, false
local keyFocus, keyboardControls = nil, {}
local syncingScroll = false
local function clearKeyFocus()
  if keyFocus and keyFocus.UnlockHighlight then keyFocus:UnlockHighlight() end
  keyFocus = nil
end
local ROW_HEIGHT = 36 -- Layout, not a result-count or performance limit.
local function readable(fontString, size)
  local font, current, flags = fontString:GetFont()
  fontString:SetFont(font, math.max(size or 12, current or 12), flags)
end
local function label(parent, value, heading)
  local row = parent:CreateFontString(nil, "OVERLAY", heading and "GameFontNormalLarge" or "GameFontHighlight")
  readable(row, heading and 16 or 13)
  row:SetJustifyH("LEFT")
  row:SetText(value)
  return row
end
local function button(parent, value, handler)
  local control = CreateFrame("Button", nil, parent, "UIPanelButtonTemplate")
  control:SetText(value)
  readable(control:GetFontString(), 13)
  control:SetSize(math.max(88, control:GetFontString():GetStringWidth() + 28), 28)
  control:SetScript("OnClick", handler)
  return control
end
local function named(value)
  return type(value) == "string" and value ~= "" and value or nil
end
local function valueText(value)
  if type(value) == "table" then
    local parts = {}
    for _, part in ipairs(value) do parts[#parts + 1] = valueText(part) end
    return table.concat(parts, ", ")
  end
  return tostring(value)
end
local function factText(fact)
  if fact.label == "prerequisitesUnknown" then return L.prerequisitesUnknown end
  if fact.label == "faction" then return L.faction .. ": " .. (L[fact.value] or L.unavailable) end
  -- The client prints a duration in the player's language.
  if fact.label == "cooldown" and type(fact.value) == "number" then return L.cooldown .. ": " .. SecondsToTime(fact.value) end
  return L[fact.label] .. ": " .. valueText(fact.value)
end
local function relationTarget(relation)
  if not relation.target then return end
  local target = lookup.Record(relation.target) or lookup.Parse(relation.target)
  if target and relation.link then
    return { key = target.key, kind = target.kind, id = target.id, name = target.name, link = relation.link }
  end
  return target
end
local function tooltip(owner, record)
  local data = lookup.Record(record.key)
  if not data then return end
  GameTooltip:SetOwner(owner, "ANCHOR_RIGHT")
  GameTooltip:SetText(named(data.name) or L.missingName)
  local detail = data.detail and data.detail()
  for _, fact in ipairs(detail and detail.fields or {}) do
    if L[fact.label] and fact.value ~= nil then
      GameTooltip:AddLine(factText(fact), 1, 1, 1, true)
    end
  end
  GameTooltip:Show()
end
local function hideRows(rows)
  for _, row in ipairs(rows) do row:Hide() end
end
local function clearDetail()
  hideRows(detailRows)
  detailRows = {} -- Owned widgets below are pooled separately by kind.
end
local textPool, linkPool = {}, {}
local textUsed, linkUsed, y = 0, 0, 0
local function addText(value, heading)
  textUsed = textUsed + 1
  local row = textPool[textUsed]
  if not row then row = label(content, ""); textPool[textUsed] = row end
  row:SetFontObject(heading and "GameFontNormalLarge" or "GameFontHighlight")
  readable(row, heading and 16 or 13)
  row:ClearAllPoints()
  row:SetPoint("TOPLEFT", content, "TOPLEFT", 0, -y)
  row:SetWidth(content:GetWidth())
  row:SetText(value)
  row:Show()
  y = y + row:GetStringHeight() + (heading and 14 or 10)
  detailRows[#detailRows + 1] = row
end
local function addLink(value, target, index)
  linkUsed = linkUsed + 1
  local row = linkPool[linkUsed]
  if not row then row = button(content, "", function(self)
    if self.target then lookup.Select(self.target); search:SetFocus() end
  end); linkPool[linkUsed] = row end
  row:ClearAllPoints()
  row:SetPoint("TOPLEFT", content, "TOPLEFT", 0, -y)
  row:SetWidth(content:GetWidth())
  row:GetFontString():SetWidth(content:GetWidth() - 24)
  row:GetFontString():SetWordWrap(true)
  row:SetText(value)
  row:SetHeight(math.max(ROW_HEIGHT, row:GetFontString():GetStringHeight() + 16))
  row.target, row.relationIndex = target, index
  row:SetScript("OnEnter", function(self) if self.target then tooltip(self, self.target) end end)
  row:SetScript("OnLeave", function() GameTooltip:Hide() end)
  row:Show()
  y = y + row:GetHeight() + 6
  detailRows[#detailRows + 1] = row
end
local function relationRows()
  local list = {}
  for _, source in ipairs({ lookup.native and lookup.native.relations or {}, lookup.detail and lookup.detail.relations or {} }) do
    for _, relation in ipairs(source) do
      if relationTarget(relation) then list[#list + 1] = relation end
    end
  end
  return list
end
local function appendRelations(relations, actionable)
  local lastType
  for _, relation in ipairs(relations) do
    if relation.type ~= lastType then addText(L[relation.type] or L.unavailable, true); lastType = relation.type end
    local target = relationTarget(relation)
    local value = relation.native and named(relation.name) or target and named(target.name) or named(relation.name) or L.missingName
    if relation.choice then value = value .. " (" .. L.choice .. ")" end
    if relation.provided then value = value .. " (" .. L.provided .. ")" end
    for _, fact in ipairs(relation.fields or {}) do
      if L[fact.label] and fact.value ~= nil then value = value .. "\n" .. factText(fact) end
    end
    if relation.quantity ~= nil then value = value .. " (" .. L.quantity .. ": " .. tostring(relation.quantity) .. ")" end
    if relation.order ~= nil then value = tostring(relation.order) .. ". " .. value end
    if target then
      actionable = actionable + 1
      addLink(value, target, actionable)
      local control = linkPool[linkUsed]
      -- A pooled native-tooltip row must never retain its old item handler.
      control:SetScript("OnClick", function(self) lookup.Select(self.target); search:SetFocus() end)
      if actionable == relationChoice then control:LockHighlight() else control:UnlockHighlight() end
    else
      addText(value)
    end
  end
  return actionable
end
local function renderDetail()
  clearDetail()
  textUsed, linkUsed, y = 0, 0, 0
  local selected, native = lookup.selected, lookup.native
  if renderedKey ~= selected.key then relationChoice = 1 end
  relationChoice = math.max(1, math.min(relationChoice, #relationRows()))
  local published = lookup.Record(selected.key)
  local nativeRelationCount = 0
  addText(native and named(native.name) or published and named(published.name) or L.missingName, true)
  if selected.kind == "item" or selected.kind == "quest" then
    addText(L.nativeDetails, true)
    if lookup.status ~= "ready" then addText(L[lookup.status] or L.unavailable) end
    if native then
      if native.objectivesUnavailable then addText(L.objectives .. ": " .. L.unavailable) end
      for _, fact in ipairs(native.fields or {}) do
        addText(factText(fact))
      end
      if native.rewardsUnavailable then addText(L.rewards .. ": " .. L.unavailable) end
      nativeRelationCount = appendRelations(native.relations or {}, 0)
    end
    if selected.kind == "item" and native and native.name then
      addLink(L.nativeDetails, nil)
      local control = linkPool[linkUsed]
      control:SetScript("OnEnter", function(self)
        GameTooltip:SetOwner(self, "ANCHOR_RIGHT")
        GameTooltip:SetHyperlink(selected.link or native.link or ("item:" .. selected.id))
        GameTooltip:Show()
      end)
      control:SetScript("OnClick", function(self) self:GetScript("OnEnter")(self) end)
    end
  end
  addText(L.referenceDetails, true)
  if lookup.packageWarning then addText(L[lookup.packageWarning]) end
  if not published then addText(L.nativeOnly) end
  for _, fact in ipairs(lookup.detail and lookup.detail.fields or {}) do
    if L[fact.label] and fact.value ~= nil then addText(factText(fact)) end
  end
  local relations = lookup.detail and lookup.detail.relations or {}
  if #relations == 0 then addText(L.missingRelations) end
  appendRelations(relations, nativeRelationCount)
  content:SetHeight(math.max(y, scroller:GetHeight()))
  scroller:SetVerticalScroll(math.min(lookup.detailScroll or 0, math.max(0, content:GetHeight() - scroller:GetHeight())))
  renderedKey = selected.key
end
local function renderResults()
  if syncingScroll then return end
  syncingScroll = true
  clearDetail()
  renderedKey = nil
  local available = math.max(1, math.floor(scroller:GetHeight() / ROW_HEIGHT))
  local total = #lookup.results
  lookup.offset = math.max(0, math.min(lookup.offset, math.max(0, total - available)))
  local restoredOffset = lookup.offset
  content:SetHeight(math.max(scroller:GetHeight(), total * ROW_HEIGHT))
  scroller:SetVerticalScroll(restoredOffset * ROW_HEIGHT)
  for index = 1, available + 1 do
    local row = resultRows[index]
    if not row then
      row = button(content, "", function(self)
        if self.record then lookup.Select(self.record); search:SetFocus() end
      end)
      row.icon = row:CreateTexture(nil, "ARTWORK")
      row.icon:SetSize(26, 26)
      row.icon:SetPoint("LEFT", 6, 0)
      row:SetScript("OnEnter", function(self) if self.record then tooltip(self, self.record) end end)
      row:SetScript("OnLeave", function() GameTooltip:Hide() end)
      resultRows[index] = row
    end
    local position = lookup.offset + index
    local record = lookup.results[position]
    if record then
      row.record = record
      row:ClearAllPoints()
      row:SetPoint("TOPLEFT", content, "TOPLEFT", 0, -(position - 1) * ROW_HEIGHT)
      row:SetSize(content:GetWidth(), ROW_HEIGHT - 2)
      local published = lookup.Record(record.key)
      row:SetText(named(record.name) or published and named(published.name) or L.missingName)
      row:GetFontString():SetPoint("LEFT", row, "LEFT", 40, 0)
      row:GetFontString():SetPoint("RIGHT", row, "RIGHT", -8, 0)
      row:GetFontString():SetJustifyH("LEFT")
      -- A quest has no item icon; the client's quest mark tells it from an item of the same name.
      row.icon:SetTexture(published and published.icon or (published or record).kind == "quest" and "Interface\\GossipFrame\\AvailableQuestIcon" or nil)
      if position == resultChoice then row:LockHighlight() else row:UnlockHighlight() end
      row:Show()
    else row:Hide() end
  end
  for index = available + 2, #resultRows do resultRows[index]:Hide() end
  syncingScroll = false
end
local function render()
  if not window or not window:IsShown() then return end
  syncing = true
  if search:GetText() ~= lookup.query then search:SetText(lookup.query) end
  syncing = false
  if copy:IsShown() and copy:GetText() ~= lookup.WebsiteRoute() then copy:Hide() end
  back:SetEnabled(#lookup.history > 0)
  retry:SetShown(lookup.selected and (lookup.selected.kind == "item" or lookup.selected.kind == "quest"))
  local route = lookup.WebsiteRoute()
  copyButton:SetEnabled(route ~= nil)
  website:SetEnabled(route ~= nil and not site.Pending())
  for _, choice in ipairs(buttons) do
    if choice.kind == lookup.filter then choice:LockHighlight() else choice:UnlockHighlight() end
  end
  hideRows(resultRows)
  if lookup.selected then
    message:SetText(lookup.packageReason and L[lookup.packageReason] or "")
    renderDetail()
  else
    message:SetText(lookup.packageReason and L[lookup.packageReason] or
      (#lookup.results == 0 and (lookup.query == "" and L.searchHint or L.empty) or ""))
    renderResults()
  end
end
local function move(delta)
  clearKeyFocus()
  if lookup.selected then
    local relations = relationRows()
    relationChoice = math.max(1, math.min(#relations, relationChoice + delta))
    render()
    for _, row in ipairs(detailRows) do
      if row.relationIndex == relationChoice then
        local _, _, _, _, top = row:GetPoint()
        scroller:SetVerticalScroll(math.min(math.max(0, -top), math.max(0, content:GetHeight() - scroller:GetHeight())))
        break
      end
    end
  else
    resultChoice = math.max(1, math.min(#lookup.results, resultChoice + delta))
    local visible = math.max(1, math.floor(scroller:GetHeight() / ROW_HEIGHT))
    if resultChoice <= lookup.offset then lookup.offset = resultChoice - 1 end
    if resultChoice > lookup.offset + visible then lookup.offset = resultChoice - visible end
    scroller:SetVerticalScroll(lookup.offset * ROW_HEIGHT)
    render()
  end
end
local function choose()
  if keyFocus and keyFocus:IsShown() and keyFocus:IsEnabled() then
    local action = keyFocus:GetScript("OnClick")
    if action then action(keyFocus) end
    return
  end
  if lookup.selected then
    local relations = relationRows()
    local relation = relations[relationChoice]
    if relation then lookup.Select(relationTarget(relation)) end
  elseif lookup.results[resultChoice] then lookup.Select(lookup.results[resultChoice]) end
end
local function create()
  if window then return end
  local start = debugprofilestop()
  window = CreateFrame("Frame", "WorldRecorderLookupFrame", UIParent, "BasicFrameTemplateWithInset")
  window:SetSize(760, 620)
  window:SetPoint("CENTER")
  window:SetClampedToScreen(true)
  window:SetMovable(true)
  window:EnableMouse(true)
  window:RegisterForDrag("LeftButton")
  window:SetScript("OnDragStart", window.StartMoving)
  window:SetScript("OnDragStop", window.StopMovingOrSizing)
  window.TitleText:SetText(L.title)
  readable(window.TitleText, 14)
  tinsert(UISpecialFrames, "WorldRecorderLookupFrame")
  window:SetScript("OnHide", function() lookup.CancelPending(); GameTooltip:Hide(); if search then search:ClearFocus() end end)
  window:Hide()
  search = CreateFrame("EditBox", nil, window, "InputBoxTemplate")
  search:SetAutoFocus(false)
  search:SetAltArrowKeyMode(false)
  search:SetFontObject("ChatFontNormal")
  local font, size, flags = search:GetFont(); search:SetFont(font, math.max(13, size), flags)
  search:SetPoint("TOPLEFT", 22, -42)
  search:SetSize(716, 30)
  search:SetScript("OnTextChanged", function(self, userInput)
    if not syncing and userInput then
      clearKeyFocus()
      resultChoice, relationChoice = 1, 1
      scroller:SetVerticalScroll(0)
      lookup.Search(self:GetText())
    end
  end)
  search:SetScript("OnEnterPressed", choose)
  search:SetScript("OnArrowPressed", function(_, direction)
    if direction == "UP" then move(-1) elseif direction == "DOWN" then move(1) end
  end)
  search:SetScript("OnEscapePressed", function()
    if copy:IsShown() then copy:Hide() elseif #lookup.history > 0 then lookup.Back() else window:Hide() end
  end)
  search:SetScript("OnTabPressed", function()
    local controls = {}
    for _, control in ipairs(keyboardControls) do
      if control:IsShown() and control:IsEnabled() then controls[#controls + 1] = control end
    end
    for index, control in ipairs(lookup.selected and detailRows or resultRows) do
      -- Result widgets are reused when scrolling. Tab only reaches complete visible
      -- rows and never scrolls them into another identity under the focused widget.
      local visible = lookup.selected or index <= math.floor(scroller:GetHeight() / ROW_HEIGHT)
      if visible and control.IsEnabled and control:IsShown() and control:IsEnabled() then controls[#controls + 1] = control end
    end
    if #controls == 0 then return end
    local at = 0
    for index, control in ipairs(controls) do if control == keyFocus then at = index end end
    if keyFocus then keyFocus:UnlockHighlight() end
    local index = ((at - 1 + (IsShiftKeyDown() and -1 or 1)) % #controls) + 1
    keyFocus = controls[index]
    keyFocus:LockHighlight()
    if lookup.selected and keyFocus:GetParent() == content then
      local _, _, _, _, top = keyFocus:GetPoint()
      scroller:SetVerticalScroll(math.min(math.max(0, -top), math.max(0, content:GetHeight() - scroller:GetHeight())))
    end
  end)
  local previous
  for index, kind in ipairs({ "all", "item", "quest" }) do
    local key = kind == "item" and "items" or kind == "quest" and "quests" or "all"
    local control = button(window, L[key], function() lookup.Search(lookup.query, kind); resultChoice = 1; search:SetFocus() end)
    if previous then control:SetPoint("LEFT", previous, "RIGHT", 8, 0) else control:SetPoint("TOPLEFT", 18, -80) end
    control.kind = kind
    buttons[#buttons + 1], previous = control, control
  end
  local context = button(window, L.context, function() WorldRecorderLookupInspect(); search:SetFocus() end)
  context:SetPoint("TOPRIGHT", -18, -118)
  back = button(window, L.back, function() lookup.Back(); search:SetFocus() end)
  back:SetPoint("TOPLEFT", 18, -118)
  retry = button(window, L.retry, function() lookup.Retry(); search:SetFocus() end)
  retry:SetPoint("LEFT", back, "RIGHT", 8, 0)
  message = label(window, "")
  message:SetPoint("TOPLEFT", 18, -154)
  message:SetWidth(720)
  scroller = CreateFrame("ScrollFrame", nil, window, "UIPanelScrollFrameTemplate")
  scroller:SetPoint("TOPLEFT", 18, -216)
  scroller:SetPoint("BOTTOMRIGHT", -38, 100)
  content = CreateFrame("Frame", nil, scroller)
  content:SetSize(700, 300)
  scroller:SetScrollChild(content)
  scroller:HookScript("OnVerticalScroll", function(_, offset)
    if syncingScroll then return end
    if not lookup.selected then clearKeyFocus(); lookup.offset = math.floor(offset / ROW_HEIGHT); renderResults() else lookup.detailScroll = offset end
  end)
  website = button(window, L.website, function()
    if site.Open(lookup.WebsiteRoute()) then render() end
    search:SetFocus()
  end)
  website:SetPoint("BOTTOMLEFT", 18, 60)
  site.OnDone = function(succeeded)
    render()
    if not succeeded then message:SetText(SCREENSHOT_FAILURE) end
  end
  copyButton = button(window, L.copyLink, function()
    local route = lookup.WebsiteRoute()
    if route then copy:Show(); copy:SetText(route); copy:HighlightText(); copy:SetFocus() end
  end)
  copyButton:SetPoint("LEFT", website, "RIGHT", 12, 0)
  copy = CreateFrame("EditBox", nil, window, "InputBoxTemplate")
  copy:SetAutoFocus(false)
  copy:SetFontObject("ChatFontNormal")
  local copyFont, copySize, copyFlags = copy:GetFont(); copy:SetFont(copyFont, math.max(13, copySize), copyFlags)
  copy:SetSize(710, 30)
  copy:SetPoint("BOTTOMLEFT", 24, 60)
  copy:SetScript("OnEscapePressed", function() copy:Hide(); search:SetFocus() end)
  copy:SetScript("OnEnterPressed", function() copy:Hide(); search:SetFocus() end)
  copy:Hide()
  keyboardControls = { buttons[1], buttons[2], buttons[3], back, retry, context, website, copyButton, window.CloseButton }
  lookup.OnChange = function() clearKeyFocus(); render() end
  lookup.metrics.createMilliseconds = debugprofilestop() - start
end
function lookup.Show()
  create()
  window:Show()
  lookup.Refresh()
  render()
  search:SetFocus()
end
function lookup.Toggle()
  if window and window:IsShown() then window:Hide() else lookup.Show() end
end
function lookup.ShowMessage(value) if window then message:SetText(value) end end

-- This optional diagnostic measures the installed client; it does not upload or save anything.
SLASH_WORLDRECORDERLOOKUPMETRICS1 = "/wrlookupmetrics"
SlashCmdList.WORLDRECORDERLOOKUPMETRICS = function()
  UpdateAddOnMemoryUsage()
  local m = lookup.metrics
  print(("World Recorder lookup: initialization %.3f ms; UI %.3f ms; last search %.3f ms; last detail %.3f ms; %d selected requests; addon %.1f KiB"):format(
    m.initializationMilliseconds, m.createMilliseconds or 0, m.searchMilliseconds, m.detailMilliseconds, m.requests, GetAddOnMemoryUsage(ADDON)))
end

lookup.metrics.initializationMilliseconds = debugprofilestop() - ns.LookupStartedAt
