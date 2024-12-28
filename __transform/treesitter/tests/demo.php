<?php

namespace App\Demo;

/**
 * Provides the ability to simplify node and data building process, batch
 * writes (if batch size is specified).
 */
#[Prototyped(property: "demo")]
class Demo
{
    public function empty(): bool
    {
        return false;
    }
}
