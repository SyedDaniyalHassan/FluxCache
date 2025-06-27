counter = 0

request = function()
  counter = counter + 1
  local key = "key-" .. counter
  local value = "value-" .. counter
  local body = string.format('{"key":"%s","value":"%s"}', key, value)
  return wrk.format("POST", "/set", {["Content-Type"]="application/json"}, body)
end