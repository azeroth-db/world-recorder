-- WOW-40: View on website. The client keeps LaunchURL for Blizzard UI, so the click draws the record's public path as a
-- packet and takes a screenshot; the World Recorder program decodes it, opens the page and deletes the screenshot.
-- Packet: "WDB2", session, click number, path length (2 bytes), the path, Adler-32 of everything before it.
-- Cells are 8 UI units on a parent scaled to the screen's 768-unit height, 120 to a row, centred near the top.
local _, ns = ...
local website = {}
ns.LookupWebsite = website

local CELL, COLUMNS, ORIGIN = 8, 120, "https://wowdb.wiki"
local strip, cells, pending = nil, {}, false
local session, sequence = GetServerTime(), 0

local function bytes(n, width)
  local out = {}
  for at = width - 1, 0, -1 do out[#out + 1] = string.char(math.floor(n / 256 ^ at) % 256) end
  return table.concat(out)
end
local function adler(value)
  local a, b = 1, 0
  for at = 1, #value do a = (a + value:byte(at)) % 65521; b = (b + a) % 65521 end
  return b * 65536 + a
end
local function packet(path)
  sequence = sequence + 1
  local body = "WDB2" .. bytes(session, 4) .. bytes(sequence, 4) .. bytes(#path, 2) .. path
  return body .. bytes(adler(body), 4)
end

local function cell(n)
  local texture = cells[n]
  if not texture then
    texture = strip:CreateTexture(nil, "OVERLAY")
    texture:SetSize(CELL, CELL)
    texture:SetPoint("TOPLEFT", (1 + (n - 1) % COLUMNS) * CELL, -(1 + math.floor((n - 1) / COLUMNS)) * CELL)
    cells[n] = texture
  end
  return texture
end
local function draw(value)
  local bits = #value * 8
  local rows = math.ceil(bits / COLUMNS)
  strip:SetSize((COLUMNS + 2) * CELL, (rows + 2) * CELL)
  for n = 1, bits do
    local at = n - 1
    local on = math.floor(value:byte(math.floor(at / 8) + 1) / 2 ^ (7 - at % 8)) % 2
    cell(n):SetColorTexture(on, on, on, 1)
    cells[n]:Show()
  end
  -- A texture is not clipped to its frame: cells a longer path drew earlier are hidden, not painted.
  for n = bits + 1, #cells do cells[n]:Hide() end
end

local function create()
  strip = CreateFrame("Frame", nil, UIParent)
  strip:SetFrameStrata("TOOLTIP")
  strip:EnableMouse(false)
  local background = strip:CreateTexture(nil, "BACKGROUND")
  background:SetAllPoints()
  background:SetColorTexture(0, 0, 0, 1)
  strip:Hide()
  strip:RegisterEvent("SCREENSHOT_SUCCEEDED")
  strip:RegisterEvent("SCREENSHOT_FAILED")
  strip:SetScript("OnEvent", function(self, event)
    if not pending then return end
    pending = false
    self:Hide()
    -- The client's "Screen captured" notice would describe our packet, not a screenshot the player took.
    if ActionStatus then ActionStatus:RegisterEvent("SCREENSHOT_SUCCEEDED") end
    if website.OnDone then website.OnDone(event == "SCREENSHOT_SUCCEEDED") end
  end)
end

-- The record's path below the site's origin; the producer owns the canonical route.
function website.Path(route)
  if type(route) ~= "string" or route:sub(1, #ORIGIN + 1) ~= ORIGIN .. "/" then return end
  local path = route:sub(#ORIGIN + 1)
  if #path < 65536 and not path:find("[%c\\]") then return path end
end

function website.Open(route)
  local path = website.Path(route)
  if not path or pending then return false end
  if not strip then create() end
  strip:SetScale(1 / UIParent:GetEffectiveScale())
  strip:ClearAllPoints()
  strip:SetPoint("TOP", UIParent, "TOP", 0, -4 * CELL)
  draw(packet(path))
  pending = true
  if ActionStatus then ActionStatus:UnregisterEvent("SCREENSHOT_SUCCEEDED") end
  strip:Show()
  -- The strip is drawn on the next frame; the screenshot waits for it.
  strip:SetScript("OnUpdate", function(self)
    self:SetScript("OnUpdate", nil)
    Screenshot()
  end)
  return true
end

function website.Pending() return pending end
