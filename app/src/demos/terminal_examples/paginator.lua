local bapp = require("bapp")

function App()
    -- Create app with custom init commands
    local init_commands = {
        btea.commands.enter_alt_screen,
        btea.commands.hide_cursor
    }

    local app = bapp.new(init_commands)

    -- Define colors
    local colors = {
        highlight = "#7D56F4",
        fg = "#CDD6F4",
        muted = "#6C7086",
        bg = "#1E1E2E"
    }

    -- Create styles
    app.styles = {
        container = btea.style()
            :padding(1)
            :background(colors.bg)
            :border(btea.borders.ROUNDED)
            :border_foreground(colors.highlight),

        title = btea.style()
            :foreground(colors.fg)
            :bold(),

        help = btea.style()
            :foreground(colors.muted)
            :italic(),

        current_page = btea.style()
            :foreground(colors.highlight)
            :bold()
    }

    -- Sample data
    app.items = {
        "First item in the list",
        "Second item with some extra text",
        "Third item that's quite descriptive",
        "Fourth item showing pagination",
        "Fifth item in our demo",
        "Sixth item with details",
        "Seventh item demonstrating scrolling",
        "Eighth item in the sequence",
        "Ninth item with information",
        "Tenth item showing more",
        "Eleventh item for testing",
        "Twelfth item in our list",
        "Thirteenth item here",
        "Fourteenth item example",
        "Fifteenth item displayed",
    }

    -- Create paginator with zero-based indexing
    app.paginator = btea.paginator({
        type = btea.paginator_types.DOTS,
        page = 0,
        per_page = 4
    })

    -- Set the total pages based on item count
    app.paginator:set_total_pages(#app.items)

    -- Define key bindings
    app.keys = {
        quit = btea.bind({
            keys = {"ctrl+c", "q", "esc"},
            help = {key = "^C/q/esc", desc = "quit"}
        }),
        prev = btea.bind({
            keys = {"left", "h", "p"},
            help = {key = "←/h/p", desc = "previous page"}
        }),
        next = btea.bind({
            keys = {"right", "l", "n"},
            help = {key = "→/l/n", desc = "next page"}
        }),
        toggle_style = btea.bind({
            keys = {"t"},
            help = {key = "t", desc = "toggle style"}
        })
    }

    -- Update function
    local function update(self, msg)
        -- Handle key events
        if type(msg) == "table" and msg.type == "update" and msg.key then
            if self.keys.quit:matches(msg) then
                return true -- signal quit
            elseif self.keys.prev:matches(msg) and not self.paginator:on_first_page() then
                self.paginator:prev_page()
            elseif self.keys.next:matches(msg) and not self.paginator:on_last_page() then
                self.paginator:next_page()
            elseif self.keys.toggle_style:matches(msg) then
                if self.paginator:get_type() == btea.paginator_types.DOTS then
                    self.paginator:set_type(btea.paginator_types.ARABIC)
                else
                    self.paginator:set_type(btea.paginator_types.DOTS)
                end
            end
        end

        return false -- continue running
    end

    -- View function
    local function view(self)
        -- Get current page bounds and items count
        local start_idx, end_idx = self.paginator:get_slice_bounds(#self.items)
        local items_on_page = self.paginator:items_on_page(#self.items)

        -- Build items list for current page
        local current_items = {}
        for i = start_idx + 1, start_idx + items_on_page do
            table.insert(current_items, string.format("%d. %s", i, self.items[i]))
        end

        -- Current page display (add 1 since we use 0-based indexing internally)
        local current_page = self.paginator:get_current_page() + 1

        -- Build navigation info
        local nav_info = string.format("Page %s (Items %d-%d of %d)",
            self.styles.current_page:render(tostring(current_page)),
            start_idx + 1,
            start_idx + items_on_page,
            #self.items
        )

        -- Build the view
        local lines = {
            self.styles.title:render("Paginator Demo"),
            "",
            nav_info,
            "",
            table.concat(current_items, "\n"),
            "",
            self.paginator:view(),
            "",
            self.styles.help:render("←/h/p previous | →/l/n next | t toggle style | ^C/q/esc quit")
        }

        return self.styles.container:render(table.concat(lines, "\n"))
    end

    -- Run the app
    app:run(update, view)
end

return App