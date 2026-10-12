-- Listens to events the player causes and keeps, per locale and build: quest givers
-- and turn-in sources, the player's position when a creature is met, and what each
-- loot source drops. Sources are keyed by kind (Creature, GameObject) and game id.
-- Every object a loot window opens is also kept as a gather with its exact position,
-- the player's last cast and the skill ranks the player had, for the world map.
local frame = CreateFrame("Frame")
local counted, killed, casts = {}, {}, {}
local lastCast, lastCastAt
local MONEY = Enum.LootSlotType.Money

-- A value the client withholds from addon code in restricted situations is skipped, never touched.
local function plain(v)
  if issecretvalue and issecretvalue(v) then return nil end
  return v
end

local function count(t)
  local n = 0
  for _ in pairs(t) do n = n + 1 end
  return n
end

local function child(t, key)
  t[key] = t[key] or {}
  return t[key]
end

local function db()
  local locale = GetLocale()
  WorldRecorderDB = WorldRecorderDB or {}
  local version, build = GetBuildInfo()
  local entry = WorldRecorderDB[locale]
  if not entry or entry.build ~= build then
    entry = { version = version, build = build, givers = {}, enders = {}, creatures = {}, loot = {} }
    WorldRecorderDB[locale] = entry
  end
  entry.gathers = entry.gathers or {}
  entry.npcs = entry.npcs or {}
  entry.sightings = entry.sightings or {}
  entry.skills = entry.skills or {}
  for _, key in ipairs({ "kills", "castLoot", "units", "abilities", "quests", "fishing" }) do
    entry[key] = entry[key] or {}
  end
  return entry
end

local function source(guid)
  guid = plain(guid)
  if not guid then return end
  local kind, _, _, _, _, id = strsplit("-", guid)
  if kind == "Creature" or kind == "GameObject" then
    return kind, tonumber(id)
  end
end

local function percent(v)
  return math.floor(v * 1000 + 0.5) / 10
end

local function position()
  if not (C_Map and C_Map.GetBestMapForUnit and C_Map.GetPlayerMapPosition) then return end
  local map = C_Map.GetBestMapForUnit("player")
  local point = map and C_Map.GetPlayerMapPosition(map, "player")
  if not (point and point.GetXY) then return end
  local x, y = point:GetXY()
  if x and y then
    return map, percent(x), percent(y)
  end
end

local function place(positions)
  local map, x, y = position()
  if not map then return end
  local list = child(positions, map)
  for _, seen in ipairs(list) do
    local dx, dy = seen[1] - x, seen[2] - y
    if dx * dx + dy * dy < 1 then return end
  end
  list[#list + 1] = { x, y }
end

local function hundredths(v)
  return math.floor(v * 10000 + 0.5) / 100
end

local function gather(id)
  local map = C_Map.GetBestMapForUnit("player")
  local point = map and C_Map.GetPlayerMapPosition(map, "player")
  -- UnitPosition's first two returns, kept in their own order: the X and Y of the client's map tables.
  local first, second, _, instance = UnitPosition("player")
  local row = { t = time(), spell = lastCast, spellAge = lastCastAt and GetTime() - lastCastAt }
  if point and point.GetXY then
    local x, y = point:GetXY()
    row.map, row.x, row.y = map, x and hundredths(x), y and hundredths(y)
  end
  if first then row.ux, row.uy, row.instance = first, second, instance end
  local list = child(db().gathers, id)
  list[#list + 1] = row
end

local function here()
  local row = {}
  local map = C_Map.GetBestMapForUnit("player")
  local point = map and C_Map.GetPlayerMapPosition(map, "player")
  if point and point.GetXY then
    local x, y = point:GetXY()
    row.map, row.x, row.y = map, x and hundredths(x), y and hundredths(y)
  end
  local first, second, _, instance = UnitPosition("player")
  if first then row.ux, row.uy, row.instance = first, second, instance end
  return row
end

-- What the open vendor window sells: item, price, stack, stock and any item or currency cost.
local function wares(npc)
  if not (GetMerchantNumItems and GetMerchantItemID) then return end
  local list = {}
  for index = 1, GetMerchantNumItems() do
    local item = GetMerchantItemID(index)
    if item then
      local info = C_MerchantFrame.GetItemInfo(index) or {}
      local row = { item = item, price = info.price, stack = info.stackCount, available = info.numAvailable,
        currency = info.currencyID, questStart = info.isQuestStartItem or nil }
      if info.hasExtendedCost then
        local costs = {}
        for cost = 1, GetMerchantItemCostInfo(index) or 0 do
          local _, value, link, currency = GetMerchantItemCostItem(index, cost)
          costs[cost] = { item = link and tonumber(link:match("item:(%d+)")), currency = currency, value = value }
        end
        row.costs = costs
      end
      list[#list + 1] = row
    end
  end
  npc.sells = list
end

-- What the open trainer window teaches, with every service type shown; the player's filter is restored.
local function lessons(npc)
  if not (GetNumTrainerServices and GetTrainerServiceInfo and GetTrainerServiceTypeFilter) then return end
  local filters = {}
  for _, kind in ipairs({ "available", "unavailable", "used" }) do
    filters[kind] = GetTrainerServiceTypeFilter(kind)
    SetTrainerServiceTypeFilter(kind, true)
  end
  local list = {}
  for index = 1, GetNumTrainerServices() do
    local name, kind, _, level, rank = GetTrainerServiceInfo(index)
    if kind ~= "header" then
      local skill, skillRank = GetTrainerServiceSkillReq(index)
      list[#list + 1] = { name = name, rank = rank, level = level, cost = GetTrainerServiceCost(index),
        skill = skill, skillRank = skillRank }
    end
  end
  npc.teaches = list
  for kind, value in pairs(filters) do SetTrainerServiceTypeFilter(kind, value and true or false) end
end

-- Text the client addressed to the player, with the player's name back as the game's own $n/$N token.
local function personal(text)
  text = plain(text)
  if not text or text == "" then return nil end
  local name = UnitName("player"):gsub("%p", "%%%0")
  return (text:gsub(name, "$n"):gsub(name:upper(), "$N"))
end

-- The flight map's nodes as this flight master offers them: name, reachability, cost from here and map position.
local function routes()
  local list = {}
  for index = 1, NumTaxiNodes() do
    local x, y = TaxiNodePosition(index)
    list[index] = { name = TaxiNodeName(index), type = TaxiNodeGetType(index), cost = TaxiNodeCost(index),
      x = x and hundredths(x), y = y and hundredths(y) }
  end
  return list
end

-- An NPC the player talks to: its name, the services the opened window shows and where the player stood.
local function interaction(role)
  local kind, id = source(UnitGUID("npc"))
  if kind ~= "Creature" or not id then return end
  local npc = child(db().npcs, id)
  npc.name = UnitName("npc")
  child(npc, "roles")[role] = true
  if role == "vendor" and CanMerchantRepair then npc.repair = CanMerchantRepair() and true or nil end
  if role == "vendor" then wares(npc) end
  if role == "trainer" then lessons(npc) end
  if role == "flightMaster" then npc.taxi = routes() end
  if role == "gossip" and C_GossipInfo and C_GossipInfo.GetOptions then
    local greeting = personal(C_GossipInfo.GetText())
    if greeting then child(npc, "gossip")[greeting] = true end
    local icons = child(npc, "gossipIcons")
    for _, option in ipairs(C_GossipInfo.GetOptions() or {}) do
      if option.icon then icons[option.icon] = option.name or true end
    end
  end
  local spot, list = here(), child(npc, "positions")
  for _, seen in ipairs(list) do
    if seen.map == spot.map and seen.x == spot.x and seen.y == spot.y then return end
  end
  list[#list + 1] = spot
end

-- An object the client makes the soft interact target lies within SoftTargetInteractRange yards of the player,
-- so herbs and veins are kept by walking past them, without the profession that gathers them.
local function sighting(guid)
  local kind, id = source(guid)
  if kind ~= "GameObject" or not id then return end
  local spot = here()
  spot.t, spot.range = time(), tonumber(GetCVar("SoftTargetInteractRange"))
  local list = child(db().sightings, id)
  for _, seen in ipairs(list) do
    if seen.map == spot.map and seen.x == spot.x and seen.y == spot.y then return end
  end
  list[#list + 1] = spot
end

local function skills()
  if not (C_SkillInfo and C_SkillInfo.GetNumSkillLines) then return end
  local ranks, changed = {}, false
  local history = db().skills
  local last = history[#history]
  for index = 1, C_SkillInfo.GetNumSkillLines() do
    local info = C_SkillInfo.GetSkillLineInfo(index)
    if info and not info.isHeader and info.skillID and info.rank then
      ranks[info.skillID] = info.rank
      if not last or last.ranks[info.skillID] ~= info.rank then changed = true end
    end
  end
  if changed then history[#history + 1] = { t = time(), ranks = ranks } end
end

local function questSource()
  return UnitGUID("questnpc") or UnitGUID("npc")
end

local function quest(book, id)
  local kind, sourceId = source(questSource())
  if not (id and id > 0 and kind and sourceId) then return end
  place(child(child(child(book, id), kind), sourceId))
end

-- Per level: health and power maxima, power type, classification and reaction to the player.
local function unitFacts(unit, id)
  local level = UnitLevel(unit)
  if not level then return end
  local row = child(child(db().units, id), level)
  row.classification, row.reaction = UnitClassification(unit), UnitReaction(unit, "player")
  row.health = plain(UnitHealthMax(unit)) or row.health
  row.powerType, row.power = UnitPowerType(unit), plain(UnitPowerMax(unit)) or row.power
end

local function creature(unit)
  local kind, id = source(UnitGUID(unit))
  if kind == "Creature" and id then
    place(child(db().creatures, id))
    unitFacts(unit, id)
  end
end

local function kill(guid, id)
  if killed[guid] then return end
  killed[guid] = true
  local kills = db().kills
  kills[id] = (kills[id] or 0) + 1
end

-- A hostile or neutral creature seen dead and not tapped by others died to the player or the player's group.
local function died(unit)
  local guid = plain(UnitGUID(unit))
  local kind, id = source(guid)
  local reaction = UnitReaction(unit, "player")
  if kind == "Creature" and UnitIsDead(unit) and not UnitIsTapDenied(unit) and reaction and reaction <= 4 then
    kill(guid, id)
  end
end

local function cast(unit, castGUID, spell)
  spell, castGUID = plain(spell), plain(castGUID)
  if unit == "player" then
    lastCast, lastCastAt = spell, GetTime()
    return
  end
  local kind, id = source(UnitGUID(unit))
  if kind ~= "Creature" or not (spell and castGUID) or casts[castGUID] then return end
  casts[castGUID] = true
  local abilities = child(db().abilities, id)
  abilities[spell] = (abilities[spell] or 0) + 1
end

local function rewards(kind)
  local list = {}
  for index = 1, (kind == "reward" and GetNumQuestRewards() or GetNumQuestChoices()) do
    local link = GetQuestItemLink(kind, index)
    local _, _, amount = GetQuestItemInfo(kind, index)
    list[index] = { item = link and tonumber(link:match("item:(%d+)")), count = amount }
  end
  return list
end

-- The open quest dialog: its texts for the player's class and race, and the rewards at the player's level.
local function dialog(field, text, offered)
  local id = GetQuestID and GetQuestID()
  if not (id and id > 0) then return end
  local row = child(db().quests, id)
  local _, class = UnitClass("player")
  local _, race = UnitRace("player")
  child(child(row, "texts"), class .. ":" .. race)[field] = personal(text)
  if not offered then return end
  local xp = GetRewardXP()
  if xp and xp > 0 then child(row, "xp")[UnitLevel("player")] = xp end
  row.money, row.rewards, row.choices = GetRewardMoney(), rewards("reward"), rewards("choice")
end

local function recentCast()
  if lastCastAt and GetTime() - lastCastAt < 3 then return lastCast end
end

local function fishingRow(entry)
  local spot, area = here(), GetSubZoneText()
  return child(child(entry.fishing, spot.map or 0), area ~= "" and area or GetZoneText())
end

-- Creature loot that follows the player's cast (skinning, pick pocket, gathering from a corpse) is kept apart
-- under that spell; the pipeline returns casts outside the gathering skill lines to plain loot.
local function lootRow(entry, kind, id, spell)
  if kind == "Creature" and spell then return child(child(child(entry.castLoot, kind), id), spell) end
  return child(child(entry.loot, kind), id)
end

local function firstOpen(row, kind, id, guid, spell)
  row.opened = (row.opened or 0) + 1
  if kind == "GameObject" then
    place(child(row, "positions"))
    gather(id)
  elseif not spell then
    kill(guid, id)
  end
end

local function take(row, slot, quantity)
  if GetLootSlotType(slot) == MONEY then
    row.money, row.moneySeen = (row.money or 0) + quantity, (row.moneySeen or 0) + 1
    return
  end
  local link = GetLootSlotLink(slot)
  local item = link and tonumber(link:match("item:(%d+)"))
  if not item then return end
  local drop = child(child(row, "items"), item)
  drop.seen, drop.count = (drop.seen or 0) + 1, (drop.count or 0) + quantity
end

local function loot()
  if not (GetNumLootItems and GetLootSourceInfo) then return end
  local entry, opened, spell = db(), {}, recentCast()
  local fishing = IsFishingLoot()
  for slot = 1, GetNumLootItems() do
    local info = { GetLootSourceInfo(slot) }
    for i = 1, #info, 2 do
      local guid, quantity = plain(info[i]), info[i + 1] or 0
      local kind, id = source(guid)
      local key = guid and guid .. ":" .. (spell or 0)
      if kind and id and not counted[key] then
        local row = fishing and fishingRow(entry) or lootRow(entry, kind, id, spell)
        if not opened[key] then
          opened[key] = true
          if fishing then row.opened = (row.opened or 0) + 1 else firstOpen(row, kind, id, guid, spell) end
        end
        take(row, slot, quantity)
      end
    end
  end
  for key in pairs(opened) do counted[key] = true end
end

local function summary()
  local entry = db()
  local sources, gathers = 0, 0
  for _, kind in pairs(entry.loot) do sources = sources + count(kind) end
  for _, list in pairs(entry.gathers) do gathers = gathers + #list end
  print(("World Recorder %s: %d quest givers, %d quest turn-ins, %d creatures, %d loot sources, %d gathers."):format(
    entry.build, count(entry.givers), count(entry.enders), count(entry.creatures), sources, gathers))
end

-- The keys bound to the interface commands the site's tool pages name, with the key text the client shows.
local COMMANDS = { "TOGGLELEGACYSYSTEM", "TOGGLETALENTS", "TOGGLESPELLBOOK", "TOGGLEPROFESSIONBOOK",
  "TOGGLEQUESTLOG", "TOGGLEWORLDMAP", "TOGGLECHARACTER0", "OPENALLBAGS" }

local function bindings()
  local keys = {}
  for _, command in ipairs(COMMANDS) do
    local first, second = GetBindingKey(command)
    keys[command] = { first = first, second = second, text = first and GetBindingText(first) }
  end
  db().bindings = { t = time(), keys = keys }
end

-- A button that writes every addon's saved data to disk without leaving the world: the client saves
-- SavedVariables on a UI reload. Shown after /worldrecorder button; drag moves it; both are kept.
local function saveButton()
  WorldRecorderUI = WorldRecorderUI or {}
  local button = CreateFrame("Button", "WorldRecorderSaveButton", UIParent, "UIPanelButtonTemplate")
  button:SetSize(80, 22)
  button:SetText(SAVE)
  local point = WorldRecorderUI.point
  if point then button:SetPoint(unpack(point)) else button:SetPoint("TOP", UIParent, "TOP", 0, -4) end
  button:SetMovable(true)
  button:SetClampedToScreen(true)
  button:RegisterForDrag("LeftButton")
  button:SetScript("OnDragStart", button.StartMoving)
  button:SetScript("OnDragStop", function(self)
    self:StopMovingOrSizing()
    local at, _, to, x, y = self:GetPoint()
    WorldRecorderUI.point = { at, "UIParent", to, x, y }
  end)
  button:SetScript("OnClick", function() ReloadUI() end)
  button:SetShown(WorldRecorderUI.button == true)
end

-- A deterministic text of a value, keys sorted, so two saves compare equal exactly when their content does.
local function fingerprint(value, out)
  if type(value) ~= "table" then
    out[#out + 1] = type(value):sub(1, 1) .. tostring(value)
    return out
  end
  local keys = {}
  for key in pairs(value) do keys[#keys + 1] = key end
  table.sort(keys, function(a, b)
    if type(a) ~= type(b) then return type(a) < type(b) end
    return a < b
  end)
  out[#out + 1] = "{"
  for _, key in ipairs(keys) do
    out[#out + 1] = tostring(key) .. "="
    fingerprint(value[key], out)
  end
  out[#out + 1] = "}"
  return out
end

-- Ranks purchased in one trait tree of one config, by trait node id: the ids the site's talents and Legacy perks carry.
local function traitRanks(config, tree)
  local ranks = {}
  for _, node in ipairs(C_Traits.GetTreeNodes(tree) or {}) do
    local info = C_Traits.GetNodeInfo(config, node)
    if info and info.ranksPurchased and info.ranksPurchased > 0 then ranks[node] = info.ranksPurchased end
  end
  return ranks
end

local function talents()
  local group = C_SpecializationInfo.GetActiveSpecGroup()
  local config = group and C_SpecializationInfo.GetCombatConfigIDForSpecGroup(group)
  local info = config and C_Traits.GetConfigInfo(config)
  if not info then return end
  local ranks = {}
  for _, tree in ipairs(info.treeIDs or {}) do
    for node, rank in pairs(traitRanks(config, tree)) do ranks[node] = rank end
  end
  return { group = group, ranks = ranks }
end

local LEGACY_TREES = { Constants.LegacyConsts.LEGACY_TREE_PROFESSIONS_ID, Constants.LegacyConsts.LEGACY_TREE_ADVENTURE_ID,
  Constants.LegacyConsts.LEGACY_TREE_PROGRESSION_ID }

local function legacy()
  local trees = {}
  for _, tree in ipairs(LEGACY_TREES) do
    local config = C_Traits.GetConfigIDByTreeID(tree)
    if config then
      local currency = {}
      for _, info in ipairs(C_Traits.GetTreeCurrencyInfo(config, tree, false) or {}) do
        currency[#currency + 1] = { id = info.traitCurrencyID, quantity = info.quantity, max = info.maxQuantity, spent = info.spent }
      end
      trees[tree] = { ranks = traitRanks(config, tree), currency = currency }
    end
  end
  return trees
end

-- Equipped items by inventory slot (1 head ... 19 tabard), with the permanent enchant from the item link.
local function gear()
  local slots = {}
  for slot = 1, 19 do
    local id = GetInventoryItemID("player", slot)
    if id then
      local link = GetInventoryItemLink("player", slot)
      slots[slot] = { id = id, enchant = link and tonumber(link:match("item:%d+:(%d+)")) }
    end
  end
  return slots
end

local function skillRanks()
  local ranks = {}
  for index = 1, C_SkillInfo.GetNumSkillLines() do
    local info = C_SkillInfo.GetSkillLineInfo(index)
    if info and not info.isHeader and info.skillID and info.rank then
      ranks[info.skillID] = { rank = info.rank, max = info.maxRank, modifier = info.modifier }
    end
  end
  return ranks
end

-- One account-wide snapshot per character, keyed by the player's GUID; t moves only when the content changes.
local function character()
  local guid = UnitGUID("player")
  if not guid then return end
  WorldRecorderCharacters = WorldRecorderCharacters or {}
  local _, class = UnitClass("player")
  local _, race, raceId = UnitRace("player")
  local quests = C_QuestLog.GetAllCompletedQuestIDs() or {}
  table.sort(quests)
  local row = {
    name = UnitName("player"), realm = GetRealmName(), class = class, race = race, raceId = raceId,
    sex = UnitSex("player"), faction = UnitFactionGroup("player"), level = UnitLevel("player"), xp = UnitXP("player"),
    xpMax = UnitXPMax("player"), money = GetMoney(), guild = GetGuildInfo("player"), map = C_Map.GetBestMapForUnit("player"),
    build = select(2, GetBuildInfo()), skills = skillRanks(), gear = gear(), talents = talents(), legacy = legacy(), quests = quests,
  }
  local old = WorldRecorderCharacters[guid]
  if old then
    row.t = old.t
    if table.concat(fingerprint(old, {})) == table.concat(fingerprint(row, {})) then return end
  end
  row.t = time()
  WorldRecorderCharacters[guid] = row
end

local function command(argument)
  if argument ~= "button" then return summary() end
  WorldRecorderUI.button = not WorldRecorderUI.button
  WorldRecorderSaveButton:SetShown(WorldRecorderUI.button)
end

local function unitEvent(unit)
  creature(unit)
  died(unit)
end

local handlers = {
  PLAYER_LOGIN = function()
    db().toc = select(4, GetBuildInfo())
    skills() bindings() summary() saveButton()
  end,
  PLAYER_ENTERING_WORLD = character,
  PLAYER_LOGOUT = character,
  UPDATE_BINDINGS = bindings,
  MERCHANT_SHOW = function() interaction("vendor") end,
  MERCHANT_UPDATE = function()
    local kind, id = source(UnitGUID("npc"))
    if kind == "Creature" and id then wares(child(db().npcs, id)) end
  end,
  TRAINER_SHOW = function() interaction("trainer") end,
  TAXIMAP_OPENED = function() interaction("flightMaster") end,
  BANKFRAME_OPENED = function() interaction("banker") end,
  PET_STABLE_SHOW = function() interaction("stableMaster") end,
  AUCTION_HOUSE_SHOW = function() interaction("auctioneer") end,
  GOSSIP_SHOW = function() interaction("gossip") end,
  PLAYER_SOFT_INTERACT_CHANGED = function(_, new) sighting(new) end,
  SKILL_LINES_CHANGED = skills,
  UNIT_SPELLCAST_SUCCEEDED = cast,
  UNIT_SPELLCAST_CHANNEL_START = cast,
  UNIT_HEALTH = died,
  UNIT_FLAGS = died,
  NAME_PLATE_UNIT_ADDED = function(unit)
    local kind, id = source(UnitGUID(unit))
    if kind == "Creature" and id then unitFacts(unit, id) end
  end,
  QUEST_DETAIL = function()
    quest(db().givers, GetQuestID and GetQuestID())
    dialog("detail", GetQuestText(), true)
    dialog("objective", GetObjectiveText())
  end,
  QUEST_PROGRESS = function() dialog("progress", GetProgressText()) end,
  QUEST_COMPLETE = function()
    quest(db().enders, GetQuestID and GetQuestID())
    dialog("completion", GetRewardText(), true)
  end,
  QUEST_TURNED_IN = function(id) quest(db().enders, id) end,
  UPDATE_MOUSEOVER_UNIT = function() unitEvent("mouseover") end,
  PLAYER_TARGET_CHANGED = function() unitEvent("target") end,
  LOOT_OPENED = loot,
}

for event in pairs(handlers) do frame:RegisterEvent(event) end
frame:SetScript("OnEvent", function(_, event, ...) handlers[event](...) end)

SLASH_WORLDRECORDER1 = "/worldrecorder"
SlashCmdList.WORLDRECORDER = command
SLASH_WORLDRECORDERSAVE1 = "/wrsave"
SlashCmdList.WORLDRECORDERSAVE = function() ReloadUI() end
