local bapp = require("bapp")

function App()
    -- Create app with custom init commands
    local init_commands = {
        btea.commands.enter_alt_screen,
        btea.commands.hide_cursor
    }

    local app = bapp.new(init_commands)

    -- Define base styles
    local base_style = btea.style()
        :border(btea.borders.ROUNDED)
        :padding(1)
        :background("#1E1E2E")
        :foreground("#CDD6F4")

    local header_style = btea.style()
        :foreground("#CBA6F7")
        :bold()
        :padding(0, 1)

    local footer_style = btea.style()
        :foreground("#6C7086")
        :italic()
        :padding(0, 1)

    -- Define text area focused styles
    local focused_styles = {
        base = btea.style()
            :border(btea.borders.ROUNDED)
            :padding(1)
            :foreground("#CDD6F4"),
        cursor_line_number = btea.style()
            :foreground("#F9E2AF")
            :bold(),
        end_of_buffer = btea.style()
            :foreground("#6C7086"),
        line_number = btea.style()
            :foreground("#89B4FA"),
        placeholder = btea.style()
            :italic()
            :foreground("#6C7086"),
        prompt = btea.style()
            :bold()
            :foreground("#89B4FA"),
        text = btea.style()
            :foreground("#CDD6F4")
    }

    -- Define text area blurred styles
    local blurred_styles = {
        base = btea.style()
            :border(btea.borders.ROUNDED)
            :padding(1)
            :foreground("#CDD6F4"),
        cursor_line = btea.style()
            :background("#313244"),
        cursor_line_number = btea.style()
            :foreground("#89B4FA"),
        end_of_buffer = btea.style()
            :foreground("#6C7086"),
        line_number = btea.style()
            :foreground("#6C7086"),
        placeholder = btea.style()
            :italic()
            :foreground("#6C7086"),
        prompt = btea.style()
            :foreground("#6C7086"),
        text = btea.style()
            :foreground("#CDD6F4")
    }

    -- Define key bindings
    app.keys = {
        quit = btea.bind({
            keys = {"ctrl+c", "esc"},
            help = {key = "^C/esc", desc = "quit"}
        }),
        submit = btea.bind({
            keys = {"ctrl+j"},
            help = {key = "^j", desc = "submit"}
        })
    }

    -- Calculate text area dimensions, leaving space for header and footer
    local HEADER_HEIGHT = 6  -- Account for header and spacing
    local FOOTER_HEIGHT = 2  -- Account for footer and spacing
    local text_area_height = app.window.height - HEADER_HEIGHT - FOOTER_HEIGHT - 4  -- Additional padding
    local text_area_width = app.window.width - 4

    -- Create text area with proper dimensions
    app.text_area = btea.text_area({
        prompt = "> ",
        placeholder = "Type your text here (Ctrl+J to submit)...",
        value = "",
        width = text_area_width,
        height = text_area_height,
        char_limit = 2000,
        show_line_numbers = true,
        focused_style = focused_styles,
        blurred_style = blurred_styles
    })

    -- Focus the text area immediately
    local focus_cmd = app.text_area:focus()
    if focus_cmd then
        app:dispatch(focus_cmd)
    end

    -- Update function
    local function update(self, msg)
        -- Update window size if changed
        if msg.window_size then
            local new_height = self.window.height - HEADER_HEIGHT - FOOTER_HEIGHT - 4
            local new_width = self.window.width - 4
            self.text_area:set_width(new_width)
            self.text_area:set_height(new_height)
        end

        -- Handle key events
        if type(msg) == "table" and msg.type == "update" and msg.key then
            if self.keys.quit:matches(msg) then
                return true -- signal quit
            elseif self.keys.submit:matches(msg) then
                local content = self.text_area:value()
                if content and content ~= "" then
                    -- Handle the submitted content here
                    print("Submitted content:", content)  -- For debugging
                    self.text_area:set_value("")
                end
            end
        end

        -- Update text area state
        local cmd = self.text_area:update(msg)
        if cmd then
            self:dispatch(cmd)
        end

        return false -- continue running
    end

    -- View function
    local function view(self)
        local lines = {
            header_style:render("Text Area Demo"),
            "",
            self.text_area:view(),
            "",
            footer_style:render("Ctrl+J to submit | ^C/Esc to quit")
        }

        return base_style:render(table.concat(lines, "\n"))
    end

    -- Run the app
    app:run(update, view)
end

return App