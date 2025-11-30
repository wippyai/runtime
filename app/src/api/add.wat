(module
  (memory (export "memory") 1)

  (func $add (export "add") (param $a i32) (param $b i32) (result i32)
    local.get $a
    local.get $b
    i32.add
  )

  (func $canonical_abi_realloc (export "canonical_abi_realloc")
    (param $old_ptr i32) (param $old_size i32) (param $align i32) (param $new_size i32)
    (result i32)
    i32.const 1024
  )

  (func $canonical_abi_free (export "canonical_abi_free")
    (param $ptr i32) (param $size i32) (param $align i32)
  )
)
