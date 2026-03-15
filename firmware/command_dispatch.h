#pragma once

#include "bridge_types.h"

namespace bridge {

bool splitCommandLine(char* rawLine, CommandFrame* frameOut);
void dispatchCommand(const CommandFrame& frame);

}  // namespace bridge