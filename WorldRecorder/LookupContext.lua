-- Observe context after native handlers; never replace their return values or modifier bindings.
local _, ns = ...
local lookup, plain = ns.Lookup, ns.LookupPlain
local function keep(link)
  link = plain(link)
  local selected = lookup.Parse(link)
  if selected then lookup.context = selected end
  return selected
end
local function tooltipContext(tooltip)
  if not tooltip or not tooltip.GetItem then return end
  local ok, _, link = pcall(tooltip.GetItem, tooltip)
  if ok then return keep(link) end
end
local function questContext(readID)
  if type(readID) ~= "function" then return end
  local ok, id = pcall(readID)
  id = ok and plain(id)
  if type(id) == "number" and id > 0 then return keep("quest:" .. id) end
end
local frame = CreateFrame("Frame")
for _, event in ipairs({ "QUEST_DETAIL", "QUEST_PROGRESS", "QUEST_COMPLETE" }) do frame:RegisterEvent(event) end
frame:SetScript("OnEvent", function() questContext(GetQuestID) end)

if hooksecurefunc then
  if type(SetItemRef) == "function" then hooksecurefunc("SetItemRef", keep) end
  for _, method in ipairs({ "SetBagItem", "SetInventoryItem", "SetHyperlink", "SetItemByID" }) do
    if GameTooltip and type(GameTooltip[method]) == "function" then
      hooksecurefunc(GameTooltip, method, function() tooltipContext(GameTooltip) end)
    end
  end
end

function lookup.CaptureContext()
  if GameTooltip and GameTooltip:IsShown() then
    local current = tooltipContext(GameTooltip)
    if current then return current end
  end
  if QuestFrame and QuestFrame:IsShown() then return questContext(GetQuestID) end
  if ItemRefTooltip and ItemRefTooltip:IsShown() then
    local current = tooltipContext(ItemRefTooltip)
    if current then return current end
  end
  -- A selected quest is read only on explicit player intent, not while opening the lookup.
  if (QuestMapFrame and QuestMapFrame:IsShown()) or not lookup.context then
    -- A visible quest surface with an unavailable identity must not inspect an
    -- unrelated item/quest left in the remembered context.
    return questContext(C_QuestLog and C_QuestLog.GetSelectedQuest)
  end
  return lookup.context
end

BINDING_HEADER_WORLDRECORDERLOOKUP = "World Recorder"
BINDING_NAME_WORLDRECORDERLOOKUP = ns.LookupText.title
BINDING_NAME_WORLDRECORDERINSPECT = ns.LookupText.context
-- No key is assigned automatically; the player chooses it in the native bindings UI.
function WorldRecorderLookupToggle()
  if lookup.Toggle then lookup.Toggle() end
end
function WorldRecorderLookupInspect()
  local context = lookup.CaptureContext()
  if lookup.Show then lookup.Show() end
  if context then lookup.Select(context) elseif lookup.ShowMessage then lookup.ShowMessage(ns.LookupText.noContext) end
end
SLASH_WORLDRECORDERLOOKUP1 = "/wrlookup"
SlashCmdList.WORLDRECORDERLOOKUP = function(query)
  if lookup.Show then lookup.Show() end
  if query and query:match("%S") then
    lookup.Search(query)
    local selected = lookup.Parse(query)
    if selected then lookup.Select(selected) end
  end
end
