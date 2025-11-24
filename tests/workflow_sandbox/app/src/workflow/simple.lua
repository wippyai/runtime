function main(input)
    local timer_req = upstream.request("timer.sleep", 5000)

    timer_req:response():receive()

    return "timer completed: " .. input
end
