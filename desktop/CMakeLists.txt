cmake_minimum_required(VERSION 3.21)
project(WorldRecorderDesktop LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_AUTOMOC ON)
set(CMAKE_AUTORCC ON)
set(APP_VERSION "0" CACHE STRING "World Recorder version, from the addon's TOC")

find_package(Qt6 REQUIRED COMPONENTS Widgets Network)

qt_add_executable(world-recorder WIN32 MACOSX_BUNDLE
    src/main.cpp src/text.cpp src/recorder.cpp src/window.cpp resources/resources.qrc)
target_compile_definitions(world-recorder PRIVATE APP_VERSION="${APP_VERSION}")
target_link_libraries(world-recorder PRIVATE Qt6::Widgets Qt6::Network)
if(APPLE)
    enable_language(OBJCXX)
    target_sources(world-recorder PRIVATE src/mac.mm)
    target_link_libraries(world-recorder PRIVATE "-framework AppKit")
endif()
set_target_properties(world-recorder PROPERTIES OUTPUT_NAME "World Recorder" MACOSX_BUNDLE OFF)
if(MINGW)
    target_link_options(world-recorder PRIVATE -static -static-libgcc -static-libstdc++)
endif()
