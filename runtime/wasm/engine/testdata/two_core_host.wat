(component
  (type $host_iface
    (instance
      (export "peek" (func (param "data" (list u8)) (result u32)))
    )
  )
  (import "test:iso/host@0.1.0" (instance $host (type $host_iface)))

  (core module $first
    (memory (export "memory") 1)
  )

  (core module $main
    (import "test:iso/host@0.1.0" "peek" (func $host_peek (param i32 i32) (result i32)))
    (memory (export "memory") 2)
    (global $heap (mut i32) (i32.const 70000))
    (func (export "cabi_realloc") (param $old_ptr i32) (param $old_size i32) (param $align i32) (param $new_size i32) (result i32)
      (local $ret i32)
      (local.set $ret (global.get $heap))
      (global.set $heap (i32.add (local.get $ret) (local.get $new_size)))
      (local.get $ret)
    )
    (func (export "echo") (param $ptr i32) (param $len i32) (result i32)
      (local $retptr i32)
      (local.set $retptr (global.get $heap))
      (global.set $heap (i32.add (global.get $heap) (i32.const 8)))
      (i32.store (local.get $retptr) (local.get $ptr))
      (i32.store (i32.add (local.get $retptr) (i32.const 4)) (local.get $len))
      (local.get $retptr)
    )
    (func (export "stamp") (param $val i32) (result i32)
      (i32.store8 (i32.const 65536) (local.get $val))
      (local.get $val)
    )
    (func (export "read-stamp") (result i32)
      (i32.load8_u (i32.const 65536))
    )
    (func (export "peek-host") (param $ptr i32) (param $len i32) (result i32)
      (call $host_peek (local.get $ptr) (local.get $len))
    )
  )

  (core module $shim
    (type $peek_t (func (param i32 i32) (result i32)))
    (table (export "$imports") 1 1 funcref)
    (func $indirect-peek (type $peek_t) (param i32 i32) (result i32)
      local.get 0
      local.get 1
      i32.const 0
      call_indirect (type $peek_t)
    )
    (export "0" (func $indirect-peek))
  )

  (core module $fixup
    (type $peek_t (func (param i32 i32) (result i32)))
    (import "" "0" (func $real_peek (type $peek_t)))
    (import "" "$imports" (table 1 1 funcref))
    (elem (i32.const 0) func $real_peek)
  )

  (core instance $first_inst (instantiate $first))
  (core instance $shim_inst (instantiate $shim))
  (alias core export $shim_inst "0" (core func $peek_stub))
  (core instance $host_stubs
    (export "peek" (func $peek_stub))
  )
  (core instance $main_inst (instantiate $main
    (with "test:iso/host@0.1.0" (instance $host_stubs))
  ))

  (alias core export $main_inst "memory" (core memory $mem))
  (alias core export $main_inst "cabi_realloc" (core func $realloc))
  (alias core export $shim_inst "$imports" (core table $imports))
  (alias export $host "peek" (func $host_peek))
  (core func $peek_lowered (canon lower (func $host_peek) (memory $mem) (realloc $realloc)))
  (core instance $fixup_args
    (export "$imports" (table $imports))
    (export "0" (func $peek_lowered))
  )
  (core instance $fixup_inst (instantiate $fixup
    (with "" (instance $fixup_args))
  ))

  (alias core export $main_inst "echo" (core func $echo_core))
  (alias core export $main_inst "stamp" (core func $stamp_core))
  (alias core export $main_inst "read-stamp" (core func $read_stamp_core))
  (alias core export $main_inst "peek-host" (core func $peek_host_core))

  (type $list_type (func (param "items" (list u8)) (result (list u8))))
  (func $echo_list_func (type $list_type)
    (canon lift (core func $echo_core) (memory $mem) (realloc $realloc))
  )
  (export "echo-list" (func $echo_list_func))

  (type $str_type (func (param "msg" string) (result string)))
  (func $echo_str_func (type $str_type)
    (canon lift (core func $echo_core) (memory $mem) (realloc $realloc))
  )
  (export "echo" (func $echo_str_func))

  (type $stamp_type (func (param "val" u32) (result u32)))
  (func $stamp_func (type $stamp_type)
    (canon lift (core func $stamp_core))
  )
  (export "stamp" (func $stamp_func))

  (type $read_stamp_type (func (result u32)))
  (func $read_stamp_func (type $read_stamp_type)
    (canon lift (core func $read_stamp_core))
  )
  (export "read-stamp" (func $read_stamp_func))

  (type $peek_host_type (func (param "data" (list u8)) (result u32)))
  (func $peek_host_func (type $peek_host_type)
    (canon lift (core func $peek_host_core) (memory $mem) (realloc $realloc))
  )
  (export "peek-host" (func $peek_host_func))
)
