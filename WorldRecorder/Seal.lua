-- A checksum of what World Recorder saves, written at logout as WorldRecorderSeal; wowdb.wiki refuses a save whose
-- records no longer give it (site/lib/contrib/seal.ts computes the same sum). A save that no longer matches its seal when
-- the game loads it was edited outside the game: it keeps the seal "edited" from then on, so playing on does not seal the
-- edit again. The code is public, so this stops edits by hand, not a forger.
local ADDON, ns = ...
local P, M = 4294967291, 1000003
local SEALED = { "WorldRecorderDB", "WorldRecorderCharacters", "WorldRecorderInstall" }
local byte, format, floor, frexp = string.byte, string.format, math.floor, math.frexp

local function feed(h, s)
  for i = 1, #s do h = (h * M + byte(s, i)) % P end
  return h
end

-- A whole number below 2^53 as its digits, any other number exactly as its binary mantissa and exponent: decimal
-- rounding differs between C and JavaScript on ties, which the client's float coordinates often are.
local function text(v)
  if type(v) ~= "number" then return tostring(v) end
  if v == floor(v) and v > -2 ^ 53 and v < 2 ^ 53 then return format("%.0f", v) end
  local m, e = frexp(v)
  return format("%.0f*2^%d", m * 2 ^ 53, e)
end

-- What the client writes to SavedVariables; anything else never reaches the file and is left out of the sum.
local WRITTEN = { string = true, number = true, boolean = true }

-- The sum of one hash per value, each fed with the value's path, so the order pairs() visits keys in does not count.
local function walk(sum, h, value)
  local kind = type(value)
  if WRITTEN[kind] then return (sum + feed(feed(h, kind:sub(1, 1)), text(value))) % P end
  if kind ~= "table" then return sum end
  for key, child in pairs(value) do
    local keyKind = type(key)
    if keyKind == "string" or keyKind == "number" then sum = walk(sum, feed(feed(h, "\31"), text(key)), child) end
  end
  return sum
end

function ns.seal(globals)
  local sum = 0
  for _, name in ipairs(SEALED) do
    if globals[name] ~= nil then sum = walk(sum, feed(0, name), globals[name]) end
  end
  return sum
end

-- World Recorder 1.7.5 is the first to seal: a save it wrote that has no seal lost it outside the game.
local function sealing(version)
  local major, minor, patch = (type(version) == "string" and version or ""):match("^(%d+)%.(%d+)%.(%d+)$")
  return major ~= nil and major * 1e6 + minor * 1e3 + patch >= 1007005
end

local function unsealed()
  for _, entry in pairs(type(WorldRecorderDB) == "table" and WorldRecorderDB or {}) do
    if type(entry) == "table" and sealing(entry.addon) then return true end
  end
  return false
end

local edited
local loaded = CreateFrame("Frame")
loaded:RegisterEvent("ADDON_LOADED")
loaded:SetScript("OnEvent", function(self, _, name)
  if name ~= ADDON then return end
  self:UnregisterEvent("ADDON_LOADED")
  local seal = WorldRecorderSeal
  edited = seal == "edited" or type(seal) == "number" and seal ~= ns.seal(_G) or seal == nil and unsealed()
end)

-- Called last at PLAYER_LOGOUT, after every record is written.
function ns.writeSeal()
  WorldRecorderSeal = edited and "edited" or ns.seal(_G)
end
