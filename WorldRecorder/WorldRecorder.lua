-- Listens to events the player causes and keeps, per locale and build: quest givers
-- and turn-in sources, the player's position when a creature is met, and what each
-- loot source drops. Sources are keyed by kind (Creature, GameObject, Item: a container opened from the bags) and game id.
-- Every object a loot window opens is also kept as a gather with its exact position,
-- the player's last cast and the skill ranks the player had, for the world map.
local ADDON, ns = ...
-- The version of this code, kept in the save: the client runs the files it loaded until the next reload, whatever
-- the folder holds by then. publish.sh checks it against the TOC.
local VERSION = "1.7.6"
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
  for _, key in ipairs({ "kills", "groupKills", "questKills", "castLoot", "units", "abilities", "quests", "fishing", "auctions", "names", "instances" }) do
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
  -- An item's GUID carries no item id; the client resolves it while the item is still in the bags.
  if kind == "Item" then
    return kind, C_Item.GetItemIDByGUID(guid)
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

local function gather(id, guid)
  -- A loot window can open without a new soft-target event. Keep only the name
  -- of this exact gathered object, never a nearby or previously selected node.
  local target = plain(UnitGUID("softinteract"))
  if target and target == guid then
    local name = plain(UnitName("softinteract"))
    if type(name) == "string" and name ~= "" then child(db().names, "GameObject")[id] = name end
  end
  local map = C_Map.GetBestMapForUnit("player")
  local point = map and C_Map.GetPlayerMapPosition(map, "player")
  -- UnitPosition's first two returns, kept in their own order: the X and Y of the client's map tables.
  local first, second, _, instance = UnitPosition("player")
  local row = { t = GetServerTime(), spell = lastCast, spellAge = lastCastAt and GetTime() - lastCastAt }
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
          -- The fourth return names a currency cost; its id is in the cost's currency link.
          local _, value, link, currencyName = GetMerchantItemCostItem(index, cost)
          local currency = currencyName and link and C_CurrencyInfo.GetCurrencyIDFromLink(link)
          costs[cost] = { item = not currencyName and link and tonumber(link:match("item:(%d+)")) or nil,
            currency = currency and currency > 0 and currency or nil, value = value }
        end
        row.costs = costs
      end
      list[#list + 1] = row
    end
  end
  npc.sells = list
end

-- What the open trainer window teaches, with every service type shown. The client sends the list after TRAINER_SHOW
-- (its own trainer window draws on TRAINER_UPDATE), so the show opens every filter and the first update with services
-- reads them, each with its spell id, then restores the player's filters.
local trainer
local SERVICE_KINDS = { "available", "unavailable", "used" }
local function lessons(npc)
  if not (GetNumTrainerServices and GetTrainerServiceInfo and GetTrainerServiceTypeFilter) then return end
  local filters = {}
  for _, kind in ipairs(SERVICE_KINDS) do filters[kind] = GetTrainerServiceTypeFilter(kind) and true or false end
  trainer = { npc = npc, filters = filters }
  for _, kind in ipairs(SERVICE_KINDS) do SetTrainerServiceTypeFilter(kind, true) end
end
local function readLessons()
  if not trainer or GetNumTrainerServices() == 0 then return end
  local list = {}
  for index = 1, GetNumTrainerServices() do
    local name, kind, _, level, rank = GetTrainerServiceInfo(index)
    if kind ~= "header" then
      local skill, skillRank = GetTrainerServiceSkillReq(index)
      local tip = C_TooltipInfo and C_TooltipInfo.GetTrainerService and C_TooltipInfo.GetTrainerService(index)
      list[#list + 1] = { name = name, rank = rank, level = level, cost = GetTrainerServiceCost(index),
        skill = skill, skillRank = skillRank, spell = tip and tip.id }
    end
  end
  local open = trainer
  trainer = nil
  open.npc.teaches = list
  for kind, value in pairs(open.filters) do SetTrainerServiceTypeFilter(kind, value) end
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

-- The open auction house: region, realm, the auctioneer's faction ("Neutral" for the Blackwater houses) and its creature
-- id. The whole-house list answers the house it was asked of, even when it arrives after the window closed.
local house, asked

local function openHouse()
  interaction("auctioneer")
  local kind, npc = source(UnitGUID("npc"))
  local realm, faction = plain(GetNormalizedRealmName()), plain(UnitFactionGroup("npc"))
  local region = plain(GetCurrentRegionName())
  house = kind == "Creature" and realm and { region = region, realm = realm, npc = npc,
    house = (faction == "Alliance" or faction == "Horde") and faction or "Neutral" } or nil
  asked = house
  C_AuctionHouse.ReplicateItems()
end

-- Launch realm names repeat across regions, so a house is keyed by region, realm and house.
local function auctionHouse(at)
  if not at then return end
  local row = child(db().auctions, (at.region or "") .. "|" .. at.realm .. "|" .. at.house)
  row.region, row.realm, row.house, row.npc = at.region, at.realm, at.house, at.npc
  return row
end

-- The client meters each addon's script time in a bucket (GetScriptBucketThrottleLimits): it refills at MaxMsPerSecond,
-- holds at most MaxMsBurst, and once it is spent halts the addon's scripts with "has exceeded its execution time
-- limit". The whole-house read therefore spends only what the bucket regained since the previous frame, less what the
-- client billed this addon in that frame, so it never takes the bucket below where it found it. The billed time is the
-- larger of the client's own profile of the addon (which also bills Blizzard code the addon's output runs) and the
-- addon's own measure of its handlers and slices. Normal and restricted limits are both honoured.
local LAST_TICK = Enum.AddOnProfilerMetric.LastTime
local spent, allowance, lastStep, deadline, started = 0, 0, 0, 0, 0
local reader, progress

local function limits()
  local at = GetScriptBucketThrottleLimits()
  return math.min(at.luaScriptBucketThrottleMaxMsPerSecondNormal, at.luaScriptBucketThrottleMaxMsPerSecondRestricted),
    math.min(at.luaScriptBucketThrottleMaxMsBurstNormal, at.luaScriptBucketThrottleMaxMsBurstRestricted)
end

-- The client fires REPLICATE_ITEM_LIST_UPDATE again after the list arrives and empties the list later, so each
-- ReplicateItems call is read once, from the first arrival that holds auctions. A list that changes between slices is
-- dropped, never saved in part. The house's `read` keeps how the latest read went, updated as it runs.
local function readHouse(asking, arrived)
  local total = C_AuctionHouse.GetNumReplicateItems()
  progress.total = total
  local function unchanged()
    if debugprofilestop() > deadline then coroutine.yield() end
    return C_AuctionHouse.GetNumReplicateItems() == total
  end
  local prices, counts, bids, read = {}, {}, {}, 0
  for index = 0, total - 1 do
    if not unchanged() then
      progress.outcome = "changed"
      return
    end
    progress.done = index
    local _, _, count, _, _, _, _, _, _, buyout, _, _, _, _, _, _, item = C_AuctionHouse.GetReplicateItemInfo(index)
    count, buyout, item = plain(count), plain(buyout), plain(item)
    if item and count and count > 0 and buyout then
      read = read + 1
      if buyout > 0 then
        local at, price = child(prices, item), math.floor(buyout / count)
        at[price] = (at[price] or 0) + count
        counts[item] = (counts[item] or 0) + 1
      else
        bids[item] = (bids[item] or 0) + 1
      end
    end
  end
  local items = {}
  for item, at in pairs(prices) do
    unchanged()
    local list = {}
    for price, quantity in pairs(at) do list[#list + 1] = { price, quantity } end
    table.sort(list, function(a, b) return a[1] < b[1] end)
    items[item] = list
  end
  auctionHouse(asking).scan = { t = arrived, total = total, auctions = read, items = items, counts = counts, bids = bids }
  progress.done, progress.outcome = total, "saved"
end

local function readStep()
  local now = debugprofilestop()
  local billed = math.max(C_AddOnProfiler.GetAddOnMetric(ADDON, LAST_TICK), spent)
  allowance = math.min(progress.burst, allowance + (now - lastStep) * progress.rate / 1000) - billed
  lastStep, spent = now, 0
  progress.frames, progress.billed = progress.frames + 1, progress.billed + billed
  progress.seconds = math.floor((now - started) / 100 + 0.5) / 10
  if allowance <= 0 then return end
  progress.slices, deadline = progress.slices + 1, now + allowance
  local ok, problem = coroutine.resume(reader)
  if not ok then
    progress.outcome = tostring(problem)
    geterrorhandler()(problem)
  end
  spent = debugprofilestop() - now
  progress.slowest = math.max(progress.slowest, spent)
  if coroutine.status(reader) == "dead" then
    reader = nil
    frame:SetScript("OnUpdate", nil)
  end
end

local function replicate()
  if not asked or C_AuctionHouse.GetNumReplicateItems() == 0 then return end
  local asking, arrived = asked, GetServerTime()
  asked = nil
  local rate, burst = limits()
  progress = { v = VERSION, t = arrived, outcome = "reading", done = 0, frames = 0, slices = 0, billed = 0, slowest = 0,
    rate = rate, burst = burst }
  auctionHouse(asking).read = progress
  started = debugprofilestop()
  allowance, lastStep = 0, started
  reader = coroutine.create(function() readHouse(asking, arrived) end)
  frame:SetScript("OnUpdate", readStep)
end

-- A browse or search result for one item: its lowest unit buyout and the quantity on sale.
local function seen(item, min, quantity)
  local row = auctionHouse(house)
  if row and min then child(row, "seen")[item] = { t = GetServerTime(), min = min, quantity = quantity } end
end

local function lower(a, b)
  if a and b then return math.min(a, b) end
  return a or b
end

local function browsed()
  local found = {}
  for _, result in ipairs(C_AuctionHouse.GetBrowseResults() or {}) do
    local key = result.itemKey
    local item, min, quantity = plain(key.itemID), plain(result.minPrice), plain(result.totalQuantity)
    if item and min and min > 0 and quantity and key.battlePetSpeciesID == 0 then
      local at = child(found, item)
      at.min, at.quantity = lower(at.min, min), (at.quantity or 0) + quantity
    end
  end
  for item, at in pairs(found) do seen(item, at.min, at.quantity) end
end

local function commodities(item)
  item = plain(item)
  if not item then return end
  local min, quantity = nil, 0
  for index = 1, C_AuctionHouse.GetNumCommoditySearchResults(item) do
    local result = C_AuctionHouse.GetCommoditySearchResultInfo(item, index)
    local price, amount = result and plain(result.unitPrice), result and plain(result.quantity)
    if price and amount then min, quantity = lower(min, price), quantity + amount end
  end
  seen(item, min, quantity)
end

local function listings(key)
  local item = key and plain(key.itemID)
  if not item or key.battlePetSpeciesID ~= 0 then return end
  local min, quantity = nil, 0
  for index = 1, C_AuctionHouse.GetNumItemSearchResults(key) do
    local result = C_AuctionHouse.GetItemSearchResultInfo(key, index)
    local buyout, amount = result and plain(result.buyoutAmount), result and plain(result.quantity)
    if amount and amount > 0 then
      quantity = quantity + amount
      if buyout and buyout > 0 then min = lower(min, math.floor(buyout / amount)) end
    end
  end
  seen(item, min, quantity)
end

-- An object the client makes the soft interact target lies within SoftTargetInteractRange yards of the player,
-- so herbs and veins are kept by walking past them, without the profession that gathers them.
local function sighting(guid)
  local kind, id = source(guid)
  if kind ~= "GameObject" or not id then return end
  -- Its name as the client shows it, so a node the reader's query cache never met is still known by name.
  local name = plain(UnitName("softinteract"))
  if name then child(db().names, kind)[id] = name end
  local spot = here()
  spot.t, spot.range = GetServerTime(), tonumber(GetCVar("SoftTargetInteractRange"))
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
  if changed then history[#history + 1] = { t = GetServerTime(), ranks = ranks } end
end

local function questSource()
  return UnitGUID("questnpc") and "questnpc" or "npc"
end

-- The source also keeps the name its quest or gossip window is titled with (UnitName of the same token).
local function quest(book, id)
  local token = questSource()
  local kind, sourceId = source(UnitGUID(token))
  id = plain(id)
  if not (id and id > 0 and kind and sourceId) then return end
  place(child(child(child(book, id), kind), sourceId))
  local name = plain(UnitName(token))
  if name then child(db().names, kind)[sourceId] = name end
end

-- The quests a gossip or greeting window lists: those it offers are given here, those it takes back are turned in here.
local function listed(available, active)
  local entry = db()
  for _, id in ipairs(available) do quest(entry.givers, id) end
  for _, id in ipairs(active) do quest(entry.enders, id) end
end

local function gossipQuests()
  local available, active = {}, {}
  for _, info in ipairs(C_GossipInfo.GetAvailableQuests()) do available[#available + 1] = info.questID end
  for _, info in ipairs(C_GossipInfo.GetActiveQuests()) do active[#active + 1] = info.questID end
  listed(available, active)
end

local function greetingQuests()
  local available, active = {}, {}
  for index = 1, GetNumAvailableQuests() do available[index] = select(5, GetAvailableQuestInfo(index)) end
  for index = 1, GetNumActiveQuests() do active[index] = GetActiveQuestID(index) end
  listed(available, active)
end

-- Inside a dungeon or raid the client keeps creature identity, health and casts from addon code; its combat log file
-- (Logs/WoWCombatLog*.txt) keeps them with advanced logging, and the uploader reads it. Logging the recorder started is
-- stopped, and the player's advanced logging setting restored, when the player leaves; a player's own logging is left alone.
local function combatLog()
  local entry = db()
  local _, kind = GetInstanceInfo()
  local inside = kind == "party" or kind == "raid"
  if inside and not LoggingCombat() then
    entry.combatLog = { advanced = GetCVar("advancedCombatLogging") }
    SetCVar("advancedCombatLogging", "1")
    LoggingCombat(true)
  elseif not inside and entry.combatLog then
    LoggingCombat(false)
    SetCVar("advancedCombatLogging", entry.combatLog.advanced)
    entry.combatLog = nil
  end
end

-- Inside an instance, the creature under the instance's map id: an instance has no UiMap, so place() keeps nothing there.
local function inInstance(id)
  local _, kind, _, _, _, _, _, map = GetInstanceInfo()
  if kind == "none" or not map then return end
  child(db().instances, map)[id] = true
end

-- Per level: health and power maxima, power type, classification and reaction to the player.
local function unitFacts(unit, id)
  inInstance(id)
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

-- The quests in the log the player has not finished: a quest item drops only to a player still on its quest.
local function openQuests()
  local list = {}
  for index = 1, C_QuestLog.GetNumQuestLogEntries() do
    local info = C_QuestLog.GetInfo(index)
    local quest = info and not info.isHeader and plain(info.questID)
    if quest and quest > 0 and not C_QuestLog.IsComplete(quest) then list[#list + 1] = quest end
  end
  return list
end

-- A kill in a group counts apart: round-robin loot opens the corpse to one member, so a group kill is a trial
-- for this player only when the player opened it (`groupOpened` on the loot row).
local function kill(guid, id)
  if killed[guid] then return end
  killed[guid] = true
  local entry = db()
  entry.kills[id] = (entry.kills[id] or 0) + 1
  if IsInGroup() then entry.groupKills[id] = (entry.groupKills[id] or 0) + 1 end
  local quests = openQuests()
  if #quests == 0 then return end
  local row = child(entry.questKills, id)
  for _, quest in ipairs(quests) do row[quest] = (row[quest] or 0) + 1 end
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
    gather(id, guid)
  elseif kind == "Creature" and not spell then
    kill(guid, id)
    if IsInGroup() then row.groupOpened = (row.groupOpened or 0) + 1 end
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
          opened[key] = kind
          if fishing then row.opened = (row.opened or 0) + 1 else firstOpen(row, kind, id, guid, spell) end
        end
        take(row, slot, quantity)
      end
    end
  end
  -- A stack keeps one GUID across opens, so each loot window of an item is an open of its own.
  for key, kind in pairs(opened) do counted[key] = kind ~= "Item" or nil end
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
  db().bindings = { t = GetServerTime(), keys = keys }
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

-- A deterministic text of a value, keys sorted, so two saves compare equal exactly when their content does; skip names
-- top-level keys left out.
local function fingerprint(value, out, skip)
  if type(value) ~= "table" then
    out[#out + 1] = type(value):sub(1, 1) .. tostring(value)
    return out
  end
  local keys = {}
  for key in pairs(value) do
    if not (skip and skip[key]) then keys[#keys + 1] = key end
  end
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

-- True while the config holds ranks the player clicked and has not applied: the server never took them, and
-- ranksPurchased already counts them.
local function staged(config)
  return config ~= nil and C_Traits.ConfigHasStagedChanges(config)
end

-- The active class trait config's ranks; while it holds staged ranks, kept (the last read) stays.
local function talents(kept)
  local group = C_SpecializationInfo.GetActiveSpecGroup()
  local config = group and C_SpecializationInfo.GetCombatConfigIDForSpecGroup(group)
  if staged(config) then return kept end
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

-- Each Legacy tree has a config of its own; a tree whose config holds staged ranks keeps its entry of kept.
local function legacy(kept)
  local trees = {}
  for _, tree in ipairs(LEGACY_TREES) do
    local config = C_Traits.GetConfigIDByTreeID(tree)
    if staged(config) then
      trees[tree] = kept and kept[tree]
    elseif config then
      local currency = {}
      for _, info in ipairs(C_Traits.GetTreeCurrencyInfo(config, tree, false) or {}) do
        currency[#currency + 1] = { id = info.traitCurrencyID, quantity = info.quantity, max = info.maxQuantity, spent = info.spent }
      end
      trees[tree] = { ranks = traitRanks(config, tree), currency = currency }
    end
  end
  return trees
end

-- An item link's item, permanent enchant, random suffix (the item string's seventh field) and bonus ids.
local function linkFields(link)
  local fields = {}
  for field in ((link:match("item:([%-%d:]+)") or "") .. ":"):gmatch("([^:]*):") do fields[#fields + 1] = tonumber(field) or 0 end
  local bonuses = {}
  for index = 1, fields[13] or 0 do bonuses[index] = fields[13 + index] end
  return fields[1], fields[2] or 0, fields[7] or 0, bonuses
end

-- Equipped items by inventory slot (1 head ... 19 tabard), with the permanent enchant, random suffix and bonus ids from the
-- item link.
local function gear()
  local slots = {}
  for slot = 1, 19 do
    local id = GetInventoryItemID("player", slot)
    if id then
      local link = GetInventoryItemLink("player", slot)
      local row = { id = id }
      if link then
        local _, enchant, suffix, bonuses = linkFields(link)
        row.enchant = enchant ~= 0 and enchant or nil
        row.suffix = suffix ~= 0 and suffix or nil
        row.bonuses = #bonuses > 0 and bonuses or nil
      end
      slots[slot] = row
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

-- The value text the character's Statistics frame shows for each statistic, read the way its own frame reads them.
local function statistics()
  local values = {}
  for key, category in pairs(GetStatisticsCategoryList() or {}) do
    if type(key) == "number" and category then
      for index = 1, GetCategoryNumAchievements(category) or 0 do
        local quantity, skip, id = GetStatistic(category, index)
        quantity = plain(quantity)
        if not skip and id and quantity and quantity ~= "" and quantity ~= "--" then values[id] = quantity end
      end
    end
  end
  return values
end

-- Progress of an unfinished achievement: criteria done of its criteria, or one criterion's quantity of its need.
local function criteriaProgress(id)
  local total = GetAchievementNumCriteria(id) or 0
  if total == 1 then
    local _, _, done, quantity, needed = GetAchievementCriteriaInfo(id, 1)
    if not done and quantity and needed and needed > 1 and quantity > 0 then return { quantity, needed } end
    return nil
  end
  local done = 0
  for index = 1, total do
    if select(3, GetAchievementCriteriaInfo(id, index)) then done = done + 1 end
  end
  if done > 0 then return { done, total } end
end

-- Earned achievements with the date the client gives (two-digit year) and the progress of unfinished ones,
-- category by category as the Legacy Challenges frame lists them.
local function achievements()
  local earned, progress = {}, {}
  for _, category in ipairs(GetCategoryList() or {}) do
    for index = 1, GetCategoryNumAchievements(category) or 0 do
      local id, _, _, completed, month, day, year = GetAchievementInfo(category, index)
      if id and completed then
        earned[id] = { y = year, m = month, d = day }
      elseif id then
        progress[id] = criteriaProgress(id)
      end
    end
  end
  return earned, progress
end

-- Title masks the character knows and the one it shows, as the character frame's title list reads them.
local function titles()
  local known = {}
  for index = 1, GetNumTitles() or 0 do
    if IsTitleKnown(index) then known[#known + 1] = index end
  end
  return { known = known, current = GetCurrentTitle() }
end

-- Mount ids the account has collected; the eleventh value of GetMountInfoByID is isCollected.
local function mounts()
  local collected = {}
  for _, id in ipairs(C_MountJournal.GetMountIDs() or {}) do
    if select(11, C_MountJournal.GetMountInfoByID(id)) then collected[#collected + 1] = id end
  end
  table.sort(collected)
  return collected
end

-- A record the game keeps; a call the client refuses leaves the field out instead of the whole snapshot.
local function kept(read)
  local ok, first, second = pcall(read)
  if ok then return first, second end
end

local function completedQuests()
  local quests = C_QuestLog.GetAllCompletedQuestIDs() or {}
  table.sort(quests)
  return quests
end

-- By PLAYER_LOGOUT, which /wrsave's reload fires too, the client has torn down equipment, skill lines, traits, completed
-- quests and the map (they read empty), and GetStatistic asserts and kills it before it writes any SavedVariables. So
-- these records are read only in the world, each again after the events that change it, and logout keeps them.
-- Each record names the fields its read fills; a read is given the row and returns its last value where a new read would
-- be wrong.
local WORLD = {
  map = { "map", read = function() return C_Map.GetBestMapForUnit("player") end },
  skills = { "skills", read = skillRanks },
  gear = { "gear", read = gear },
  quests = { "quests", read = completedQuests },
  talents = { "talents", read = function(row) return talents(row.talents) end },
  legacy = { "legacy", read = function(row) return legacy(row.legacy) end },
  statistics = { "statistics", read = function() return kept(statistics) end },
  titles = { "titles", read = function() return kept(titles) end },
  mounts = { "mounts", read = function() return kept(mounts) end },
  achievements = { "achievements", "progress", read = function() return kept(achievements) end },
}
local EVERY = {}
for name in pairs(WORLD) do EVERY[#EVERY + 1] = name end
-- Appended records: kept until the build changes, as the recorder's other lists are, and left out of the change test.
local APPENDED = { sheets = true, talentSets = true }

-- From PLAYER_LEAVING_WORLD (a loading screen, or the teardown before logout) to the next PLAYER_ENTERING_WORLD the
-- records read empty, so nothing is read then.
local inWorld = false

local function readInto(row, record)
  if not inWorld then return end
  local values = { record.read(row) }
  for index, field in ipairs(record) do row[field] = values[index] end
end

-- One account-wide snapshot per character, keyed by the player's GUID; t moves only when the content changes.
-- names lists the WORLD records read now; the others keep their last read.
local function character(names)
  local guid = UnitGUID("player")
  if not guid then return end
  WorldRecorderCharacters = WorldRecorderCharacters or {}
  local _, class = UnitClass("player")
  local _, race, raceId = UnitRace("player")
  local row = {
    name = UnitName("player"), realm = GetRealmName(), class = class, race = race, raceId = raceId,
    sex = UnitSex("player"), faction = UnitFactionGroup("player"), level = UnitLevel("player"), xp = UnitXP("player"),
    xpMax = UnitXPMax("player"), money = GetMoney(), guild = GetGuildInfo("player"), build = select(2, GetBuildInfo()),
  }
  local old = WorldRecorderCharacters[guid]
  for _, record in pairs(WORLD) do
    for _, field in ipairs(record) do row[field] = old and old[field] end
  end
  for _, name in ipairs(names) do readInto(row, WORLD[name]) end
  if old and old.build == row.build then row.sheets, row.talentSets = old.sheets, old.talentSets end
  if old then
    row.t = old.t
    if table.concat(fingerprint(old, {}, APPENDED)) == table.concat(fingerprint(row, {}, APPENDED)) then return end
  end
  row.t = GetServerTime()
  WorldRecorderCharacters[guid] = row
end

-- Re-reads the named records of the character's snapshot after an event that changes them, without the full read.
local function refresh(...)
  local row = WorldRecorderCharacters and WorldRecorderCharacters[UnitGUID("player")]
  if not row then return end
  local names = { ... }
  local function content()
    local out = {}
    for _, name in ipairs(names) do
      for _, field in ipairs(WORLD[name]) do fingerprint(row[field], out) end
    end
    return table.concat(out)
  end
  local before = content()
  for _, name in ipairs(names) do readInto(row, WORLD[name]) end
  if content() ~= before then row.t = GetServerTime() end
end

-- A value the save can hold: one the client does not withhold, and a finite number where it is a number (the uploader
-- reads only plain decimals).
local function storable(v)
  v = plain(v)
  if v ~= v or v == math.huge or v == -math.huge then return nil end
  return v
end

-- Every return of one call in its position; a withheld or non-finite return leaves its position empty.
local function returns(...)
  local list = {}
  for index = 1, select("#", ...) do list[index] = storable((select(index, ...))) end
  if next(list) then return list end
end

-- One guarded call per index from first to last, Lua index 1 holding first; nothing when no call answered.
local function each(read, first, last)
  local list = {}
  for index = first, last do
    local ok, value = pcall(read, index)
    if ok then list[index - first + 1] = value end
  end
  if next(list) then return list end
end

-- The highest combat rating index among the client's CR_* globals, found once a session.
local lastRating
local function ratings()
  if not lastRating then
    local last = 0
    for name, value in pairs(_G) do
      if type(value) == "number" and type(name) == "string" and name:sub(1, 3) == "CR_" then last = math.max(last, value) end
    end
    lastRating = last > 0 and last or nil
  end
  return lastRating or 0
end

-- The Character Info values, from the calls Forever's own pane makes (blizzard_uipanels_game/camelot/paperdollframe.lua,
-- paperdollframestats.lua), keyed by API name, every return in its position and nothing rounded.
local SHEET = {
  UnitStat = function() return each(function(stat) return returns(UnitStat("player", stat)) end, 1, 5) end,
  UnitArmor = function() return returns(UnitArmor("player")) end,
  UnitAttackPower = function() return returns(UnitAttackPower("player")) end,
  UnitRangedAttackPower = function() return returns(UnitRangedAttackPower("player")) end,
  UnitAttackSpeed = function() return returns(UnitAttackSpeed("player")) end,
  UnitRangedDamage = function() return returns(UnitRangedDamage("player")) end,
  UnitDefenseSkill = function() return returns(UnitDefenseSkill("player")) end,
  -- Lua index i holds school i - 1 (0 physical through 6 arcane).
  UnitResistance = function() return each(function(school) return returns(UnitResistance("player", school)) end, 0, 6) end,
  GetCritChance = function() return storable(GetCritChance()) end,
  GetRangedCritChance = function() return storable(GetRangedCritChance()) end,
  GetHitModifier = function() return storable(GetHitModifier()) end,
  GetSpellHitModifier = function() return storable(GetSpellHitModifier()) end,
  GetRangedHitModifier = function() return storable(GetRangedHitModifier()) end,
  GetMeleeHaste = function() return storable(GetMeleeHaste()) end,
  GetRangedHaste = function() return returns(GetRangedHaste()) end,
  UnitSpellHaste = function() return storable(UnitSpellHaste("player")) end,
  GetDodgeChance = function() return storable(GetDodgeChance()) end,
  GetParryChance = function() return storable(GetParryChance()) end,
  GetBlockChance = function() return storable(GetBlockChance()) end,
  GetShieldBlock = function() return storable(GetShieldBlock()) end,
  GetSpellBonusHealing = function() return storable(GetSpellBonusHealing()) end,
  UnitHealthMax = function() return storable(UnitHealthMax("player")) end,
  UnitPowerMax = function() return storable(UnitPowerMax("player", 0)) end,
  GetSpellCritChance = function() return each(function(school) return storable(GetSpellCritChance(school)) end, 1, 7) end,
  GetSpellBonusDamage = function() return each(function(school) return storable(GetSpellBonusDamage(school)) end, 1, 7) end,
  GetCombatRating = function() return each(function(index) return storable(GetCombatRating(index)) end, 1, ratings()) end,
  GetCombatRatingBonus = function() return each(function(index) return storable(GetCombatRatingBonus(index)) end, 1, ratings()) end,
}

-- Every helpful and harmful aura on the player as {spellId, applications}, sorted; a spell the client withholds leaves
-- the whole list out, since a list missing one aura would pass for complete.
local function auras()
  local list = {}
  for _, filter in ipairs({ "HELPFUL", "HARMFUL" }) do
    for _, aura in ipairs(C_UnitAuras.GetUnitAuras("player", filter) or {}) do
      list[#list + 1] = { assert(storable(aura.spellId)), storable(aura.applications) }
    end
  end
  table.sort(list, function(a, b)
    if a[1] ~= b[1] then return a[1] < b[1] end
    return (a[2] or 0) < (b[2] or 0)
  end)
  return list
end

-- Each occupied inventory slot as "itemID:enchantID" from its item link; an empty enchant field is enchant 0.
local function wearing()
  local slots = {}
  for slot = 1, 19 do
    local link = plain(GetInventoryItemLink("player", slot))
    if link and link:match("item:%d+") then
      local item, enchant, suffix, bonuses = linkFields(link)
      slots[slot] = item .. ":" .. enchant .. (suffix ~= 0 and ":" .. suffix or "") .. (#bonuses > 0 and ";" .. table.concat(bonuses, ",") or "")
    end
  end
  return slots
end

-- Each weapon slot's temporary enchants (poisons, oils, sharpening stones), as Forever's buff frame reads them, keyed by
-- Enum.WeaponSlot; a slot without one is an empty list. timeLeft is in milliseconds.
local function weaponEnchants()
  local slots = {}
  for _, slot in pairs(Enum.WeaponSlot) do
    local list = {}
    for index, enchant in ipairs(C_Item.GetWeaponEnchantInfo(slot)) do
      list[index] = {
        hasEnchant = storable(enchant.hasEnchant),
        enchantType = storable(enchant.enchantType),
        timeLeft = storable(enchant.timeLeft),
        charges = storable(enchant.charges),
        enchantID = storable(enchant.enchantID),
        enchantIconID = storable(enchant.enchantIconID),
      }
    end
    slots[slot] = list
  end
  return slots
end

-- The stable text of a talents() record: its group, then each purchased node and rank in node order.
local function talentKey(set)
  local nodes, parts = {}, {}
  for node in pairs(set.ranks) do nodes[#nodes + 1] = node end
  table.sort(nodes)
  for _, node in ipairs(nodes) do parts[#parts + 1] = node .. "=" .. set.ranks[node] end
  return set.group .. "|" .. table.concat(parts, ",")
end

-- The facts an observation is read under, each through its own guarded call.
local FACTS = {
  level = function() return storable(UnitLevel("player")) end,
  race = function() return storable((select(3, UnitRace("player")))) end,
  class = function() return storable((select(3, UnitClass("player")))) end,
  form = function() return storable(GetShapeshiftFormID() or 0) end,
  gear = wearing,
  weaponEnchants = weaponEnchants,
  auras = auras,
}

-- What each guarded call answered, by name; a refused call leaves its name out.
local function answers(calls)
  local values = {}
  for name, call in pairs(calls) do
    local ok, value = pcall(call)
    if ok then values[name] = value end
  end
  return values
end

-- Appends one observation to the character's sheets: at login, at each level gained and after each equipment change the
-- client applied.
local function observe(reason)
  local row = WorldRecorderCharacters and WorldRecorderCharacters[UnitGUID("player")]
  if not row then return end
  local entry = answers(FACTS)
  entry.t, entry.reason, entry.sheet = GetServerTime(), reason, answers(SHEET)
  local set = kept(talents)
  local ok, key = pcall(talentKey, set)
  if set and ok then
    child(row, "talentSets")[key] = set
    entry.talents = key
  end
  local sheets = child(row, "sheets")
  sheets[#sheets + 1] = entry
  row.t = entry.t
end

-- How each house's latest whole-house read went: outcome, auctions read of listed, time taken and the bucket it paced to.
local function reads()
  for key, row in pairs(db().auctions) do
    local at = row.read
    if at then
      print(("World Recorder %s %s: %s, %d/%d auctions, %.1f s, %d slices, slowest %.1f ms, %.0f ms billed, limit %s ms/s burst %s ms."):format(
        at.v, key, at.outcome, at.done, at.total or 0, at.seconds or 0, at.slices, at.slowest, at.billed, at.rate, at.burst))
    end
  end
end

local function command(argument)
  if argument == "auctions" then return reads() end
  if argument ~= "button" then return summary() end
  WorldRecorderUI.button = not WorldRecorderUI.button
  WorldRecorderSaveButton:SetShown(WorldRecorderUI.button)
end

local function unitEvent(unit)
  creature(unit)
  died(unit)
end

-- The save's own id, made once and kept: the site files a save under it however the save reaches the site, so a save
-- sent by the program and from the browser counts once.
local function install()
  if type(WorldRecorderInstall) == "string" and #WorldRecorderInstall == 32 and WorldRecorderInstall:match("^[0-9a-f]+$") then return end
  local digits = {}
  for i = 1, 32 do digits[i] = string.format("%x", math.random(0, 15)) end
  WorldRecorderInstall = table.concat(digits)
end

local handlers = {
  PLAYER_LOGIN = function()
    install()
    db().toc, db().addon = select(4, GetBuildInfo()), VERSION
    skills() bindings() summary() saveButton()
    -- The World Recorder program marks the copy it installs; any other copy names the program.
    if not C_AddOns.GetAddOnMetadata(ADDON, "X-Program") then print(ns.LookupText.program) end
  end,
  PLAYER_ENTERING_WORLD = function()
    inWorld = true
    character(EVERY)
    combatLog()
  end,
  PLAYER_LEAVING_WORLD = function() inWorld = false end,
  -- The seal goes last and is written even when the snapshot fails, or the next login would take the save for edited.
  PLAYER_LOGOUT = function()
    xpcall(function() character({}) end, geterrorhandler())
    ns.writeSeal()
  end,
  ACHIEVEMENT_EARNED = function() character(EVERY) end,
  PLAYER_LEVEL_UP = function() character(EVERY) end,
  ZONE_CHANGED_NEW_AREA = function()
    character(EVERY)
    combatLog()
  end,
  -- Trait configs loaded after entering the world, a talent or Legacy change the player applied, a switch of talent group.
  TRAIT_CONFIG_LIST_UPDATED = function() refresh("talents", "legacy") end,
  TRAIT_CONFIG_UPDATED = function() refresh("talents", "legacy") end,
  ACTIVE_TALENT_GROUP_CHANGED = function() refresh("talents", "legacy") end,
  PLAYER_EQUIPMENT_CHANGED = function() refresh("gear") end,
  NEW_MOUNT_ADDED = function() refresh("mounts") end,
  KNOWN_TITLES_UPDATE = function() refresh("titles") end,
  UPDATE_BINDINGS = bindings,
  MERCHANT_SHOW = function() interaction("vendor") end,
  MERCHANT_UPDATE = function()
    local kind, id = source(UnitGUID("npc"))
    if kind == "Creature" and id then wares(child(db().npcs, id)) end
  end,
  TRAINER_SHOW = function() interaction("trainer"); readLessons() end,
  TRAINER_UPDATE = readLessons,
  TRAINER_CLOSED = function() trainer = nil end,
  TAXIMAP_OPENED = function() interaction("flightMaster") end,
  BANKFRAME_OPENED = function() interaction("banker") end,
  PET_STABLE_SHOW = function() interaction("stableMaster") end,
  AUCTION_HOUSE_SHOW = openHouse,
  AUCTION_HOUSE_CLOSED = function() house = nil end,
  REPLICATE_ITEM_LIST_UPDATE = replicate,
  AUCTION_HOUSE_BROWSE_RESULTS_UPDATED = browsed,
  AUCTION_HOUSE_BROWSE_RESULTS_ADDED = browsed,
  COMMODITY_SEARCH_RESULTS_UPDATED = commodities,
  COMMODITY_SEARCH_RESULTS_ADDED = commodities,
  ITEM_SEARCH_RESULTS_UPDATED = listings,
  ITEM_SEARCH_RESULTS_ADDED = listings,
  GOSSIP_SHOW = function()
    interaction("gossip")
    gossipQuests()
  end,
  QUEST_GREETING = greetingQuests,
  PLAYER_SOFT_INTERACT_CHANGED = function(_, new) sighting(new) end,
  SKILL_LINES_CHANGED = function()
    skills()
    refresh("skills")
  end,
  UNIT_SPELLCAST_SUCCEEDED = cast,
  UNIT_SPELLCAST_CHANNEL_START = cast,
  UNIT_HEALTH = died,
  UNIT_FLAGS = died,
  NAME_PLATE_UNIT_ADDED = function(unit)
    local kind, id = source(UnitGUID(unit))
    if kind == "Creature" and id then unitFacts(unit, id) end
  end,
  -- An item-started quest has no creature or object giver; whatever unit token is set then is not one.
  QUEST_DETAIL = function(startItem)
    if not startItem or startItem == 0 then quest(db().givers, GetQuestID and GetQuestID()) end
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
frame:SetScript("OnEvent", function(_, event, ...)
  local start = debugprofilestop()
  handlers[event](...)
  spent = spent + debugprofilestop() - start
end)

-- An observation is read at the end of the frame, as Character Info updates (PaperDollFrame_QueuedUpdate), so it holds
-- every update the client applied in that frame. An equipment change in the world waits for the player's first
-- UNIT_STATS or UNIT_INVENTORY_CHANGED in its own frame or a later one; GetTime() is the same throughout one frame.
local watch = CreateFrame("Frame")
local due, entered, changedAt, appliedAt, levelled

local function flush()
  local start = debugprofilestop()
  watch:SetScript("OnUpdate", nil)
  -- In combat the client withholds Character Info values, so the reading waits for the player to leave combat.
  if UnitAffectingCombat("player") then
    spent = spent + debugprofilestop() - start
    return
  end
  local reason = due
  due, changedAt = nil, nil
  observe(reason)
  spent = spent + debugprofilestop() - start
end

-- A login in the same frame as an equipment change or a level is the login observation.
local function queue(reason)
  if reason == "login" or not due then due = reason end
  watch:SetScript("OnUpdate", flush)
end

-- A level gained is read once the client shows the new level, at its stat update.
local function applied()
  appliedAt = GetTime()
  if levelled and UnitLevel("player") >= levelled then
    levelled = nil
    queue("level")
  end
  if changedAt then queue("equip") end
end

local watchers = {
  PLAYER_ENTERING_WORLD = function(initialLogin, reloadingUi)
    entered = true
    if initialLogin or reloadingUi then queue("login") end
  end,
  -- While the client loads the character it is not yet in the world; its equipment is the login observation's.
  PLAYER_EQUIPMENT_CHANGED = function()
    if not entered then return end
    changedAt = GetTime()
    if appliedAt == changedAt then queue("equip") end
  end,
  PLAYER_LEVEL_UP = function(level) levelled = level end,
  PLAYER_REGEN_ENABLED = function() if due then watch:SetScript("OnUpdate", flush) end end,
  UNIT_STATS = applied,
  UNIT_INVENTORY_CHANGED = applied,
  -- Nothing is read at logout.
  PLAYER_LOGOUT = function()
    due, changedAt, levelled = nil, nil, nil
    watch:SetScript("OnUpdate", nil)
  end,
}

for _, event in ipairs({ "PLAYER_ENTERING_WORLD", "PLAYER_EQUIPMENT_CHANGED", "PLAYER_LEVEL_UP", "PLAYER_REGEN_ENABLED", "PLAYER_LOGOUT" }) do watch:RegisterEvent(event) end
watch:RegisterUnitEvent("UNIT_STATS", "player")
watch:RegisterUnitEvent("UNIT_INVENTORY_CHANGED", "player")
watch:SetScript("OnEvent", function(_, event, ...)
  local start = debugprofilestop()
  watchers[event](...)
  spent = spent + debugprofilestop() - start
end)

SLASH_WORLDRECORDER1 = "/worldrecorder"
SlashCmdList.WORLDRECORDER = command
SLASH_WORLDRECORDERSAVE1 = "/wrsave"
SlashCmdList.WORLDRECORDERSAVE = function() ReloadUI() end
