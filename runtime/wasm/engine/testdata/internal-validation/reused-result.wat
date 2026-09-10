(component
  (core module $first (;0;)
    (memory (;0;) 1)
    (export "memory" (memory 0))
  )
  (core module $main (;1;)
    (type (;0;) (func (param i32 i32 i32 i32) (result i32)))
    (type (;1;) (func (param i32 i32) (result i32)))
    (memory (;0;) 2)
    (global $heap (;0;) (mut i32) i32.const 70000)
    (export "memory" (memory 0))
    (export "cabi_realloc" (func 0))
    (export "trap" (func 1))
    (export "echo" (func 2))
    (func (;0;) (type 0) (param $old_ptr i32) (param $old_size i32) (param $align i32) (param $new_size i32) (result i32)
      (local $ret i32)
      global.get $heap
      local.set $ret
      local.get $ret
    )
    (func (;1;) (type 1) (param $ptr i32) (param $len i32) (result i32)
      local.get $ptr
      i32.const 0
      i32.store8
      unreachable
    )
    (func (;2;) (type 1) (param $ptr i32) (param $len i32) (result i32)
      (local $retptr i32)
      i32.const 71000
      local.set $retptr
      local.get $retptr
      local.get $ptr
      i32.store
      local.get $retptr
      i32.const 4
      i32.add
      local.get $len
      i32.store
      local.get $retptr
    )
  )
  (core instance $first_inst (;0;) (instantiate $first))
  (core instance $main_inst (;1;) (instantiate $main))
  (alias core export $main_inst "memory" (core memory $mem (;0;)))
  (alias core export $main_inst "cabi_realloc" (core func $realloc (;0;)))
  (alias core export $main_inst "echo" (core func $echo_core (;1;)))
  (alias core export $main_inst "trap" (core func $trap_core (;2;)))
  (type $str_type (;0;) (func (param "msg" string) (result string)))
  (func $echo_func (;0;) (type $str_type) (canon lift (core func $echo_core) (memory $mem) (realloc $realloc)))
  (export (;1;) "echo" (func $echo_func))
  (type (;1;) (list u8))
  (type (;2;) (list u8))
  (type $list_type (;3;) (func (param "items" 1) (result 2)))
  (func $echo_list_func (;2;) (type $list_type) (canon lift (core func $echo_core) (memory $mem) (realloc $realloc)))
  (export (;3;) "echo-list" (func $echo_list_func))
  (func $trap_string (;4;) (type $str_type) (canon lift (core func $trap_core) (memory $mem) (realloc $realloc)))
  (export (;5;) "trap-string" (func $trap_string))
  (func $trap_list (;6;) (type $list_type) (canon lift (core func $trap_core) (memory $mem) (realloc $realloc)))
  (export (;7;) "trap-list" (func $trap_list))
)
