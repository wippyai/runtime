(component
  (core module $core1 (;0;)
    (type (;0;) (func (result i32)))
    (type (;1;) (func (param i32)))
    (type (;2;) (func))
    (type (;3;) (func (param i32 i32 i32 i32) (result i32)))
    (type (;4;) (func (param i32 i32) (result i32)))
    (memory (;0;) 2)
    (global $heap1 (;0;) (mut i32) i32.const 70000)
    (global $post1 (;1;) (mut i32) i32.const 0)
    (global $async1 (;2;) (mut i32) i32.const 0)
    (export "memory" (memory 0))
    (export "asyncify_get_state" (func 0))
    (export "asyncify_start_unwind" (func 1))
    (export "asyncify_stop_unwind" (func 2))
    (export "asyncify_start_rewind" (func 3))
    (export "asyncify_stop_rewind" (func 4))
    (export "cabi_realloc" (func 5))
    (export "cabi_post_func1" (func 6))
    (export "get_post1" (func 7))
    (export "get_heap1" (func 8))
    (export "func1" (func 9))
    (func (;0;) (type 0) (result i32)
      global.get $async1
    )
    (func (;1;) (type 1) (param i32)
      i32.const 1
      global.set $async1
    )
    (func (;2;) (type 2)
      i32.const 0
      global.set $async1
    )
    (func (;3;) (type 1) (param i32)
      i32.const 2
      global.set $async1
    )
    (func (;4;) (type 2)
      i32.const 0
      global.set $async1
    )
    (func (;5;) (type 3) (param $old_ptr i32) (param $old_size i32) (param $align i32) (param $new_size i32) (result i32)
      (local $ret i32)
      global.get $heap1
      local.set $ret
      local.get $ret
      local.get $new_size
      i32.add
      global.set $heap1
      local.get $ret
    )
    (func (;6;) (type 1) (param $ptr i32)
      global.get $post1
      i32.const 1
      i32.add
      global.set $post1
    )
    (func (;7;) (type 0) (result i32)
      global.get $post1
    )
    (func (;8;) (type 0) (result i32)
      global.get $heap1
    )
    (func (;9;) (type 4) (param $ptr i32) (param $len i32) (result i32)
      (local $retptr i32)
      global.get $heap1
      local.set $retptr
      global.get $heap1
      i32.const 8
      i32.add
      global.set $heap1
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
  (core module $core2 (;1;)
    (type (;0;) (func (result i32)))
    (type (;1;) (func (param i32)))
    (type (;2;) (func))
    (type (;3;) (func (param i32 i32 i32 i32) (result i32)))
    (type (;4;) (func (param i32 i32) (result i32)))
    (memory (;0;) 3)
    (global $heap2 (;0;) (mut i32) i32.const 140000)
    (global $post2 (;1;) (mut i32) i32.const 0)
    (global $async2 (;2;) (mut i32) i32.const 0)
    (export "memory" (memory 0))
    (export "asyncify_get_state" (func 0))
    (export "asyncify_start_unwind" (func 1))
    (export "asyncify_stop_unwind" (func 2))
    (export "asyncify_start_rewind" (func 3))
    (export "asyncify_stop_rewind" (func 4))
    (export "cabi_realloc" (func 5))
    (export "cabi_post_func2" (func 6))
    (export "get_post2" (func 7))
    (export "get_heap2" (func 8))
    (export "func2" (func 9))
    (func (;0;) (type 0) (result i32)
      global.get $async2
    )
    (func (;1;) (type 1) (param i32)
      i32.const 1
      global.set $async2
    )
    (func (;2;) (type 2)
      i32.const 0
      global.set $async2
    )
    (func (;3;) (type 1) (param i32)
      i32.const 2
      global.set $async2
    )
    (func (;4;) (type 2)
      i32.const 0
      global.set $async2
    )
    (func (;5;) (type 3) (param $old_ptr i32) (param $old_size i32) (param $align i32) (param $new_size i32) (result i32)
      (local $ret i32)
      global.get $heap2
      local.set $ret
      local.get $ret
      local.get $new_size
      i32.add
      global.set $heap2
      local.get $ret
    )
    (func (;6;) (type 1) (param $ptr i32)
      global.get $post2
      i32.const 1
      i32.add
      global.set $post2
    )
    (func (;7;) (type 0) (result i32)
      global.get $post2
    )
    (func (;8;) (type 0) (result i32)
      global.get $heap2
    )
    (func (;9;) (type 4) (param $ptr i32) (param $len i32) (result i32)
      (local $retptr i32)
      global.get $heap2
      local.set $retptr
      global.get $heap2
      i32.const 8
      i32.add
      global.set $heap2
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
  (core instance $inst1 (;0;) (instantiate $core1))
  (core instance $inst2 (;1;) (instantiate $core2))
  (alias core export $inst1 "memory" (core memory $mem1 (;0;)))
  (alias core export $inst1 "cabi_realloc" (core func $realloc1 (;0;)))
  (alias core export $inst1 "func1" (core func $func1_core (;1;)))
  (alias core export $inst1 "cabi_post_func1" (core func $post1_core (;2;)))
  (alias core export $inst1 "get_post1" (core func $get_post1_core (;3;)))
  (alias core export $inst1 "get_heap1" (core func $get_heap1_core (;4;)))
  (alias core export $inst2 "memory" (core memory $mem2 (;1;)))
  (alias core export $inst2 "cabi_realloc" (core func $realloc2 (;5;)))
  (alias core export $inst2 "func2" (core func $func2_core (;6;)))
  (alias core export $inst2 "cabi_post_func2" (core func $post2_core (;7;)))
  (alias core export $inst2 "get_post2" (core func $get_post2_core (;8;)))
  (alias core export $inst2 "get_heap2" (core func $get_heap2_core (;9;)))
  (type $str_type (;0;) (func (param "msg" string) (result string)))
  (type $u32_type (;1;) (func (result u32)))
  (func $lift1 (;0;) (type $str_type) (canon lift (core func $func1_core) (memory $mem1) (realloc $realloc1) (post-return $post1_core)))
  (export (;1;) "func1" (func $lift1))
  (func $lift2 (;2;) (type $str_type) (canon lift (core func $func2_core) (memory $mem2) (realloc $realloc2) (post-return $post2_core)))
  (export (;3;) "func2" (func $lift2))
  (func $lift_get_post1 (;4;) (type $u32_type) (canon lift (core func $get_post1_core)))
  (export (;5;) "get-post1" (func $lift_get_post1))
  (func $lift_get_post2 (;6;) (type $u32_type) (canon lift (core func $get_post2_core)))
  (export (;7;) "get-post2" (func $lift_get_post2))
  (func $lift_get_heap1 (;8;) (type $u32_type) (canon lift (core func $get_heap1_core)))
  (export (;9;) "get-heap1" (func $lift_get_heap1))
  (func $lift_get_heap2 (;10;) (type $u32_type) (canon lift (core func $get_heap2_core)))
  (export (;11;) "get-heap2" (func $lift_get_heap2))
)
