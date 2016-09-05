package main

import (
    "fmt"
    "time"
    "github.com/titos-carrasco/MindWave-Go"
)

func main() {
    // colocar el headset unos 4 minutos antes para que se estabilice
    mw := mindwave.NewMindWave( "/dev/ttyUSB0", 0xF6, 0x4F )
    err := mw.Connect()
    if( err == nil ) {
        for i:=0; i<=30000; i=i+1 {
            // requerido para el scheduler
            time.Sleep( time.Millisecond * 1 )
            fmt.Printf( "%06d ==> Signal: %03d, Attention: %03d, Meditation: %03d, RawWave: %d\n",
                        i, mw.PoorSignalQuality(), mw.AttentionESense(), mw.MeditationESense(), mw.RawWave16Bit() )
        }
        mw.Disconnect()
    }
}

